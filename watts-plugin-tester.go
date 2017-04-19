package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	v "github.com/gima/govalid/v1"
	"github.com/imdario/mergo"
	"github.com/kalaspuffar/base64url"
	"gopkg.in/alecthomas/kingpin.v2"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type pluginInput map[string](*json.RawMessage)
type pluginOutput struct {
	outputBytes []byte
	err         error
	duration    string
}

type globalOutput map[string](*json.RawMessage)

var (
	exitCode                     = 0
	exitCodePluginError          = 1
	exitCodePluginExecutionError = 2
	exitCodeInternalError        = 3
	exitCodeUserError            = 4

	app          = kingpin.New("watts-plugin-tester", "Test tool for watts plugins")
	pluginAction = app.Flag("plugin-action", "The plugin action to run the plugin with").Default("parameter").Short('a').String()

	inputComplementFile   = app.Flag("json-file", "Complement the plugin input with a json file").Short('j').String()
	inputComplementString = app.Flag("json", "Complement the plugin input with a json object (provided as a string)").String()
	inputComplementConf   = app.Flag("config", "Complement the plugin input with the config parameters from a watts config").Short('c').String()
	inputComplementConfID = app.Flag("config-identifier", "Service ID for the watts config").Short('i').String()

	machineReadable        = app.Flag("machine", "Be machine readable (all output will be json)").Short('m').Bool()
	useEnvForParameterPass = app.Flag("env", "Use this environment variable to pass the plugin input to the plugin").Short('e').Bool()
	envVarForParameterPass = app.Flag("env-var", "This environment variable is used to pass the plugin input to the plugin").Default("WATTS_PARAMETER").String()

	pluginCheck     = app.Command("check", "Check a plugin against the inbuilt typed schema")
	pluginCheckName = pluginCheck.Arg("pluginName", "Name of the plugin to check").Required().String()

	pluginTest     = app.Command("test", "Test a plugin against the inbuilt typed schema and expected output values")
	pluginTestName = pluginTest.Arg("pluginName", "Name of the plugin to test").Required().String()

	printDefault     = app.Command("default", "Print the default plugin input as json")
	printSpecific    = app.Command("specific", "Print the plugin input (including the user override) as json")

	generateDefault    = app.Command("generate", "Generate a fitting json input file for the given plugin")
	pluginGenerateName = generateDefault.Arg("pluginName", "Name of the plugin to generate a default json for").Required().String()

	outputMessages = []json.RawMessage{}

	// for MarshalIndent
	outputIndentation = "                 "
	outputTabWidth    = "    "

	defaultwattVersionString = "1.0.0"
	defaultWattsVersion     = toRawJSONString(defaultwattVersionString)
	defaultCredentialState  = toRawJSONString("undefined")
	defaultConfParams       = json.RawMessage(`{}`)
	defaultParams           = json.RawMessage(`{}`)
	defaultAdditionalLogins = json.RawMessage(`[]`)
	defaultUserInfo         = json.RawMessage(`{
		"iss": "https://issuer.example.com",
		"sub": "123456789"
	}`)

	defaultAction      = json.RawMessage(`"parameter"`)
	defaultWattsUserID = json.RawMessage(``)

	defaultPluginInput = pluginInput{
		"watts_version":     &defaultWattsVersion,
		"cred_state":        &defaultCredentialState,
		"conf_params":       &defaultConfParams,
		"params":            &defaultParams,
		"user_info":         &defaultUserInfo,
		"additional_logins": &defaultAdditionalLogins,
	}

	schemeAccessToken = v.Optional(v.String())
	schemeUserInfo    = v.Object(
		v.ObjKV("iss", v.String()),
		v.ObjKV("sub", v.String()),
	)
	schemeAdditionalLogins = v.Array(v.ArrEach(
		v.Object(
			v.ObjKV("user_info", schemeUserInfo),
			v.ObjKV("access_token", schemeAccessToken),
		),
	))
	schemeParams = v.Object(
		v.ObjKeys(v.String(v.StrRegExp("^[a-z0-9_]+$"))),
	)
	schemeCredential = v.Object(
		v.ObjKV("name", v.String()),
		v.ObjKV("type", v.String()),
		v.ObjKV("value", v.String()),
		v.ObjKV("save_as", v.Optional(v.String())),
		v.ObjKV("rows", v.Optional(v.Number())),
		v.ObjKV("cols", v.Optional(v.Number())),
	)
	schemeRequestParam = v.Object(
		v.ObjKV("key", v.String()),
		v.ObjKV("name", v.String()),
		v.ObjKV("description", v.String()),
		v.ObjKV("type", v.String()),
		v.ObjKV("mandatory", v.Boolean()),
	)
	pluginInputScheme = v.Object(
		v.ObjKV("watts_version", v.String()),
		v.ObjKV("watts_userid", v.String()),
		v.ObjKV("cred_state", v.String()),
		v.ObjKV("access_token", schemeAccessToken),
		v.ObjKV("additional_logins", schemeAdditionalLogins),
		v.ObjKV("conf_params", schemeParams),
		v.ObjKV("params", schemeParams),
		v.ObjKV("user_info", schemeUserInfo),
	)
	schemeRequestResultValue = v.Object(v.ObjKV("result", v.Or(
		v.String(v.StrIs("error")),
		v.String(v.StrIs("oidc_login")),
		v.String(v.StrIs("ok")),
	)))
	schemesRequest = map[string]v.Validator{
		"error": v.Object(
			v.ObjKV("result", v.String(v.StrIs("error"))),
			v.ObjKV("user_msg", v.String()),
			v.ObjKV("log_msg", v.String()),
		),
		"oidc_login": v.Object(
			v.ObjKV("result", v.String(v.StrIs("oidc_login"))),
			v.ObjKV("provider", v.String()),
			v.ObjKV("msg", v.String()),
		),
		"ok": v.Object(
			v.ObjKV("result", v.String(v.StrIs("ok"))),
			v.ObjKV("credential", v.Array(v.ArrEach(schemeCredential))),
			v.ObjKV("state", v.String()),
		),
	}

	wattsSchemes = map[string](map[string]v.Validator){
		"1.0.0": map[string]v.Validator{
			"parameter": v.Object(
				v.ObjKV("result", v.String(v.StrIs("ok"))),
				v.ObjKV("version", v.String()),
				v.ObjKV("conf_params", v.Array(v.ArrEach(
					v.Object(
						v.ObjKV("name", v.String()),
						v.ObjKV("type", v.String()),
						v.ObjKV("default", v.String()),
					),
				))),
				v.ObjKV("request_params", v.Array(v.ArrEach(
					v.Array(v.ArrEach(schemeRequestParam)),
				))),
			),
			"request": v.Function(validateRequestScheme),
			"revoke": v.Or(
				v.Object(
					v.ObjKV("result", v.String(v.StrIs("error"))),
					v.ObjKV("user_msg", v.String()),
					v.ObjKV("log_msg", v.String()),
				),
				v.Object(
					v.ObjKV("result", v.String(v.StrIs("ok"))),
				),
			),
		}, // end of "1.0.0"

	}
)

func check(err error, exitCode int, msg string) {
	if err != nil {
		if msg != "" {
			app.Errorf("%s - %s", err, msg)
		} else {
			app.Errorf("%s", err)
		}
		os.Exit(exitCode)
	}
	return
}

func validateRequestScheme(data interface{}) (path string, err error) {
	path, err = schemeRequestResultValue.Validate(data)
	if err != nil {
		return
	}

	resultValue := data.(map[string]interface{})["result"].(string)
	return schemesRequest[resultValue].Validate(data)
}

func validatePluginAction(action string) {
	if action != "request" && action != "parameter" && action != "revoke" {
		app.Errorf("invalid plugin action %s", action)
		os.Exit(exitCodeUserError)
	}
}

func (p *pluginInput) validate() {
	var bs []byte
	var i interface{}

	bs, err := json.MarshalIndent(p, outputIndentation, outputTabWidth)
	check(err, exitCodeInternalError, "marshal error")

	err = json.Unmarshal(bs, &i)
	check(err, exitCodeInternalError, "unmarshal error")
	path, err := pluginInputScheme.Validate(i)

	if err != nil {
		app.Errorf("Unable to validate plugin input")
		fmt.Printf("%s: %s\n", "Plugin Input", bs)
		fmt.Printf("%s: %s\n", "Error", err)
		fmt.Printf("%s: %s\n", "Path", path)
		os.Exit(exitCodePluginError)
	} 

	return
}

func (p *pluginInput) generateUserID() {
	userIDJSON := map[string](*json.RawMessage){}
	userIDJSONReduced := map[string](*json.RawMessage){}

	userInfo := *(*p)["user_info"]

	err := json.Unmarshal(userInfo, &userIDJSON)
	check(err, exitCodeInternalError, "Error unmarshaling watts_userid")

	userIDJSONReduced["issuer"] = userIDJSON["iss"]
	userIDJSONReduced["subject"] = userIDJSON["sub"]

	j, err := json.Marshal(userIDJSONReduced)
	check(err, exitCodeInternalError, "")

	escaped := bytes.Replace(j, []byte{'/'}, []byte{'\\', '/'}, -1)
	defaultWattsUserID = toRawJSONString(base64url.Encode(escaped))
	(*p)["watts_userid"] = &defaultWattsUserID
	return
}

func (p *pluginInput) setPluginAction() {
	defaultAction = toRawJSONString(*pluginAction)
	(*p)["action"] = &defaultAction
	return
}

func (p *pluginInput) marshalPluginInput() (s []byte) {
	s, err := json.MarshalIndent(*p, outputTabWidth, outputTabWidth)
	check(err, exitCodeInternalError, fmt.Sprintf("unable to marshal '%s'", s))
	return
}

func (p *pluginInput) specifyPluginInput() {

	// merge a user provided watts config
	if *inputComplementConf != "" {
		if *inputComplementConfID != "" {
			fileContent, err := ioutil.ReadFile(*inputComplementConf)
			check(err, exitCodeUserError, "")

			regex := fmt.Sprintf("service.%s.plugin.(?P<key>.+) = (?P<value>.+)\n",
				*inputComplementConfID)
			configExtractor, err := regexp.Compile(regex)
			check(err, exitCodeInternalError, "")

			matches := configExtractor.FindAllSubmatch(fileContent, 10)

			if len(matches) > 0 {
				confParams := map[string]string{}
				for i := 1; i < len(matches); i++ {
					confParams[string(matches[i][1])] = string(matches[i][2])
				}

				confParamsJSON, err := json.Marshal(confParams)
				check(err, exitCodeInternalError, "Formatting conf parameters")

				defaultConfParams = json.RawMessage(confParamsJSON)
			} else {
				app.Errorf("Could not find configuration parameters for '%s' in '%s'",
					*inputComplementConfID, *inputComplementConf)
				os.Exit(exitCodeUserError)
			}

		} else {
			app.Errorf("Need a config identifier for config override")
			os.Exit(exitCodeUserError)
		}
	}

	// merge a user provided json string
	if *inputComplementString != "" {

		overridePluginInput := pluginInput{}
		err := json.Unmarshal([]byte(*inputComplementString), &overridePluginInput)
		check(err, exitCodeUserError, "on unmarshaling user provided json")

		err = mergo.Merge(&overridePluginInput, p)
		check(err, exitCodeInternalError, "on merging user provided json")

		*p = overridePluginInput
	}

	// merge a user provided json file
	if *inputComplementFile != "" {
		overrideBytes, err := ioutil.ReadFile(*inputComplementFile)
		check(err, exitCodeUserError, "")

		overridePluginInput := pluginInput{}
		err = json.Unmarshal(overrideBytes, &overridePluginInput)
		check(err, exitCodeUserError, "on unmarshaling user provided json file")

		err = mergo.Merge(&overridePluginInput, p)
		check(err, exitCodeInternalError, "on merging user provided json file")

		*p = overridePluginInput
	}

	p.generateUserID()
	p.setPluginAction()
	p.validate()
}

func (p *pluginInput) version() (version string) {
	versionJSON := (*p)["watts_version"]
	versionBytes, err := json.Marshal(&versionJSON)
	check(err, exitCodeInternalError, "")

	versionExtractor, _ := regexp.Compile("[^\"+v]+")
	extractedVersion := versionExtractor.Find(versionBytes)

	if _, versionFound := wattsSchemes[string(extractedVersion)]; !versionFound {
		extractedVersion = versionExtractor.Find(defaultWattsVersion)
		(*p)["watts_version"] = &defaultWattsVersion
	}

	version = string(extractedVersion)
	return
}

func (p *pluginInput) executePlugin(pluginName string) (output pluginOutput) {
	pluginInputJSON := p.marshalPluginInput()
	inputBase64 := base64.StdEncoding.EncodeToString(pluginInputJSON)

	var cmd *exec.Cmd
	if *useEnvForParameterPass {
		cmd = exec.Command(pluginName)
		cmd.Env = []string{fmt.Sprintf("%s=%s", *envVarForParameterPass, inputBase64)}
	} else {
		cmd = exec.Command(pluginName, inputBase64)
	}

	timeBeforeExec := time.Now()
	outputBytes, err := cmd.CombinedOutput()
	timeAfterExec := time.Now()
	duration := fmt.Sprintf("%s", timeAfterExec.Sub(timeBeforeExec))

	return pluginOutput{outputBytes, err, duration}
}

func (p *pluginInput) checkPlugin(pluginName string) (output globalOutput) {
	output = globalOutput{}

	output.print("plugin_name", pluginName)
	output.printJSON("input", json.RawMessage(p.marshalPluginInput()))

	pluginOutput := p.executePlugin(pluginName)
	if pluginOutput.err != nil {
		output.print("result", "error")
		output.print("error", fmt.Sprint(pluginOutput.err))
		output.printArbitrary("output", string(pluginOutput.outputBytes))
		output.print("description", "error executing the plugin")
		exitCode = 3
		return
	}

	output.printJSON("output", byteToRawMessage(pluginOutput.outputBytes))
	output.print("time", pluginOutput.duration)

	var pluginOutputInterface interface{}
	err := json.Unmarshal(pluginOutput.outputBytes, &pluginOutputInterface)
	if err != nil {
		output.print("result", "error")
		output.print("description", "error processing the output of the plugin")
		output.printArbitrary("error", fmt.Sprintf("%s", err))
		exitCode = 1
		return
	}

	path, err := wattsSchemes[p.version()][*pluginAction].Validate(pluginOutputInterface)
	if err != nil {
		output.print("result", "error")
		output.print("description", fmt.Sprintf("validation error %s", err))
		output.print("path", path)
		exitCode = 1
		return
	}

	output.print("result", "ok")
	output.print("description", "validation passed")
	return
}

func (o *globalOutput) printJSON(a string, b json.RawMessage) {
	if !*machineReadable {
		bs, err := json.MarshalIndent(&b, outputIndentation, outputTabWidth)
		if err != nil {
			fmt.Printf("%15s: %s\n%15s\n\n", a, string(b), fmt.Sprintf("end of %s", a))
		} else {
			fmt.Printf("%15s: %s\n%15s\n\n", a, string(bs), fmt.Sprintf("end of %s", a))
		}
		return
	}
	outputMessages = append(outputMessages, b)
	(*o)[a] = &(outputMessages[len(outputMessages)-1])

}
func (o *globalOutput) print(a string, b string) {
	if !*machineReadable {
		fmt.Printf("%15s: %s\n", a, b)
		return
	}

	m := toRawJSONString(b)
	outputMessages = append(outputMessages, m)
	(*o)[a] = &(outputMessages[len(outputMessages)-1])
}
func (o *globalOutput) printArbitrary(a string, b string) {
	if !*machineReadable {
		fmt.Printf("%15s: %s\n", a, b)
		return
	}

	m := toRawJSONString(escapeJSONString(b))
	outputMessages = append(outputMessages, m)
	(*o)[a] = &(outputMessages[len(outputMessages)-1])
}

func (o globalOutput) String() string {
	if !*machineReadable {
		return ""
	}

	bs, err := json.MarshalIndent(&o, "", outputTabWidth)
	if err != nil {
		return fmt.Sprintf("error producing machine readable output: %s\n", err)
	}

	return fmt.Sprintf("%s", string(bs))
}

func (o *globalOutput) toDefaultJSON() {
	fmt.Printf("%s %T", (*o), (*o))
	return
}

func byteToRawMessage(inputBytes []byte) (rawMessage json.RawMessage) {
	rawMessage = json.RawMessage(``)

	testJSONObject := map[string]interface{}{}
	err := json.Unmarshal(inputBytes, &testJSONObject)
	if err != nil {
		rawMessage = toRawJSONString(escapeJSONString(string(inputBytes)))
	} else {
		err = json.Unmarshal(inputBytes, &rawMessage)
		if err != nil {
			app.Errorf("unmarshal successful, but bad json conversion: '%s'\n", string(inputBytes))
			rawMessage = toRawJSONString("got erroneous output")
		}
	}
	return
}

func toRawJSONString(str string) (jo json.RawMessage) {
	jo = json.RawMessage(fmt.Sprintf("\"%s\"", str))
	return
}

func escapeJSONString(s string) (e string) {
	e = strings.Replace(s, "\n", "", -1)
	e = strings.Replace(e, "\"", "\\\"", -1)
	return
}

/*
* all plugin input generation shall take place here
 */
func main() {
	app.Author("Lukas Burgey @ KIT within the INDIGO DataCloud Project")
	app.Version("0.4.0")

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginCheck.FullCommand():
		defaultPluginInput.specifyPluginInput()
		fmt.Printf("%s", defaultPluginInput.checkPlugin(*pluginCheckName))

	case generateDefault.FullCommand():
		defaultPluginInput.specifyPluginInput()

		pluginOutput := defaultPluginInput.executePlugin(*pluginGenerateName)

		m := map[string]interface{}{}
		err := json.Unmarshal(pluginOutput.outputBytes, &m)
		check(err, 1, "foo")
		confParams := m["conf_params"].([]interface{})

		generatedConfig := map[string](interface{}){}
		for _, v := range confParams {
			mm := v.(map[string]interface{})
			generatedConfig[mm["name"].(string)] = mm["default"].(string)
		}

		b, err := json.Marshal(generatedConfig)
		check(err, exitCodeInternalError, "")
		defaultConfParams = byteToRawMessage(b)

		defaultPluginInput.validate()

		fmt.Printf("%s", defaultPluginInput.marshalPluginInput())

	case printDefault.FullCommand():
		fmt.Printf("%s", defaultPluginInput.marshalPluginInput())

	case printSpecific.FullCommand():
		defaultPluginInput.specifyPluginInput()

		fmt.Printf("%s", defaultPluginInput.marshalPluginInput())
	}

	os.Exit(exitCode)
}
