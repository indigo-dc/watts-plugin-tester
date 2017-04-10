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
	"time"
)

type PluginInput map[string](*json.RawMessage)

type Output map[string](*json.RawMessage)

var (
	exitCode = 0
	exitCodePluginError = 1
	exitCodePluginExecutionError = 2
	exitCodeInternalError = 3
	exitCodeUserError = 4

	app                 = kingpin.New("watts-plugin-tester", "Test tool for watts plugins")
	pluginTestAction    = app.Flag("plugin-action", "The plugin action to be tested. Defaults to \"parameter\"").Default("parameter").Short('a').String()
	pluginInputOverride = app.Flag("json", "Use an user provided json file to override the default one").Short('j').String()
	machineReadable = app.Flag("machine", "Be machine readable (all output will be json)").Short('m').Bool()

	pluginTest     = app.Command("test", "Test a plugin")
	pluginTestName = pluginTest.Arg("pluginName", "Name of the plugin to test").Required().String()

	printDefault     = app.Command("default", "Print the default plugin input as json")
	validateDefault  = printDefault.Flag("validate", "Validate the produced json").Short('v').Bool()
	printSpecific    = app.Command("specific", "Print the plugin input (including the user override) as json")
	validateSpecific = printSpecific.Flag("validate", "Validate the produced json").Short('v').Bool()

	outputMessages = []json.RawMessage{}

	// for MarshalIndent
	outputIndentation = "                 "
	outputTabWidth    = "    "

	defaultWattsVersion     = json.RawMessage(`"1.0.0"`)
	defaultCredentialState  = json.RawMessage(`"undefined"`)
	defaultConfParams       = json.RawMessage(`{}`)
	defaultParams           = json.RawMessage(`{}`)
	defaultAdditionalLogins = json.RawMessage(`[]`)
	defaultUserInfo         = json.RawMessage(`{
		"iss": "https://issuer.example.com",
		"sub": "123456789"
	}`)

	defaultAction      = json.RawMessage(`"parameter"`)
	defaultWattsUserId = json.RawMessage(``)

	defaultPluginInput = PluginInput{
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

func validateRequestScheme(data interface{}) (path string, err error) {
	path, err = schemeRequestResultValue.Validate(data)
	if err != nil {
		return
	}

	resultValue := data.(map[string]interface{})["result"].(string)
	return schemesRequest[resultValue].Validate(data)
}

func (p *PluginInput) validate() {
	var er error
	var bs []byte
	var i interface{}

	bs, er = json.MarshalIndent(p, outputIndentation, outputTabWidth)
	if er != nil {
		app.Errorf("plugin input:\n%s\n", *p)
		app.Errorf("bytes:\n%s\n", bs)
		app.Errorf("error marshaling:\n%s\n", er)
		os.Exit(exitCodeInternalError)
	}

	json.Unmarshal(bs, &i)
	path, err := pluginInputScheme.Validate(i)

	if err != nil {
		app.Errorf("Unable to validate plugin input\n")
		app.Errorf("%s: %s\n", "Plugin Input", bs)
		app.Errorf("%s: %s\n", "Error", err)
		app.Errorf("%s: %s\n", "Path", path)
		os.Exit(exitCodePluginError)
	} else {
		if *validateSpecific || *validateDefault {
			fmt.Printf("Plugin input validation passed\n")
		}
	}

	return
}

func (p *PluginInput) generateUserID() {
	userIdJson := map[string](*json.RawMessage){}
	userIdJsonReduced := map[string](*json.RawMessage){}

	userInfo := *(*p)["user_info"]
	//fmt.Printf("user_info: %s\n", userInfo)

	err := json.Unmarshal(userInfo, &userIdJson)
	if err != nil {
		app.Errorf("Error unmarshaling watts_userid: %s\n", err)
		os.Exit(exitCodeInternalError)
	}

	//fmt.Printf("uid:%s\n", userIdJson)

	userIdJsonReduced["issuer"] = userIdJson["iss"]
	userIdJsonReduced["subject"] = userIdJson["sub"]

	j, err := json.Marshal(userIdJsonReduced)
	//fmt.Printf("reduced uid:%s\n", j)

	escaped := bytes.Replace(j, []byte{'/'}, []byte{'\\', '/'}, -1)
	st := fmt.Sprintf("\"%s\"", base64url.Encode(escaped))
	defaultWattsUserId = json.RawMessage(st)
	(*p)["watts_userid"] = &defaultWattsUserId
	return
}

func (p *PluginInput) marshalPluginInput() (s []byte) {
	var err error

	s, err = json.MarshalIndent(*p, outputIndentation, outputTabWidth)
	if err != nil {
		app.Errorf("Unable to marshal: Error (%s)\n%s\n", err, s)
		os.Exit(exitCodeInternalError)
	}
	return
}

func (p *PluginInput) specifyPluginInput(path string) {
	p = &defaultPluginInput

	if path == "" {
		return
	}

	overrideBytes, err := ioutil.ReadFile(*pluginInputOverride)
	if err != nil {
		app.Errorf("Error reading user provided file ", *pluginInputOverride, " (", err, ")")
		os.Exit(exitCodeUserError)
	}

	overridePluginInput := PluginInput{}
	err = json.Unmarshal(overrideBytes, &overridePluginInput)
	if err != nil {
		app.Errorf("Error unmarshaling user provided json: ", *pluginInputOverride, " (", err, ")")
		os.Exit(exitCodeUserError)
	}

	err = mergo.Merge(&overridePluginInput, p)
	if err != nil {
		app.Errorf("Error merging: (", err, ")")
		os.Exit(exitCodeInternalError)
	}

	*p = overridePluginInput
	return
}

func (p *PluginInput) doPluginTest(pluginName string) (output Output) {
	output = Output{}

	var wattsVersion string
	rv := (*p)["watts_version"]
	v, err := json.Marshal(&rv)
	if err == nil {
		wattsVersion = string(bytes.Replace(v, []byte{'"'}, []byte{}, -1))
		if _, versionFound := wattsSchemes[wattsVersion]; !versionFound {
			wattsVersion = string(bytes.Replace(defaultWattsVersion, []byte{'"'}, []byte{}, -1))
		}
	} else {
		app.Errorf("%s", err)
		os.Exit(exitCodeInternalError)
	}

	pluginInputJson := p.marshalPluginInput()

	output.print("plugin_name", pluginName)
	output.print("action", *pluginTestAction)
	output.printJson("input", json.RawMessage(pluginInputJson))

	inputBase64 := base64.StdEncoding.EncodeToString(pluginInputJson)

	timeBeforeExec := time.Now()
	pluginOutput, err := exec.Command(pluginName, inputBase64).CombinedOutput()
	timeAfterExec := time.Now()
	execDuration := timeAfterExec.Sub(timeBeforeExec)
	if err != nil {
		output.print("result", "error")
		output.print("error", fmt.Sprint(err))
		output.print("description", "error executing the plugin")
		exitCode = 3
		return
	}

	output.printJson("output", byteToRawMessage(pluginOutput))
	//fmt.Printf("pluginOutput: %s\n", pluginOutput)

	/*
		pluginOutputJson := json.RawMessage(``)
		err = json.Unmarshal(pluginOutput, &pluginOutputJson)
		if err != nil {
			output.print("output", string(pluginOutput))
		} else {
			output.printJson("output", pluginOutputJson)
		}

		fmt.Printf("Output: %s\n", output)
	*/

	var pluginOutputInterface interface{}
	err = json.Unmarshal(pluginOutput, &pluginOutputInterface)
	if err != nil {
		output.print("result", "error")
		output.print("error", fmt.Sprintf("%s", err))
		output.print("description", "error processing the output of the plugin (into an interface)")
		exitCode = 1
		return
	}

	output.print("time", fmt.Sprint(execDuration))

	path, errr := wattsSchemes[wattsVersion][*pluginTestAction].Validate(pluginOutputInterface)
	if errr != nil {
		output.print("result", "error")
		output.print("description", fmt.Sprintf("Validation error at %s. Error (%s)", path, errr))
		exitCode = 1
		return
	} else {
		output.print("result", "ok")
		output.print("description", "validation passed")
	}

	return
}

func (o *Output) printJson(a string, b json.RawMessage) {
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
func (o *Output) print(a string, b string) {
	if !*machineReadable {
		fmt.Printf("%15s: %s\n", a, b)
		return
	}

	m := json.RawMessage(fmt.Sprintf("\"%s\"", b))
	outputMessages = append(outputMessages, m)
	(*o)[a] = &(outputMessages[len(outputMessages)-1])
}

func (o Output) String() string {
	if !*machineReadable {
		return ""
	}

	bs, err := json.MarshalIndent(&o, "", outputTabWidth)
	if err != nil {
		return fmt.Sprintf("error producing machine readable output: %s\n%s\n", err)
	} else {
		return fmt.Sprintf("%s", string(bs))
	}
}

func byteToRawMessage(inputBytes []byte) (rawMessage json.RawMessage) {
	testJsonObject := map[string]interface{}{}
	err := json.Unmarshal(inputBytes, &testJsonObject)
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("got invalid json: '%s'\n", string(inputBytes)))
		rawMessage = json.RawMessage(fmt.Sprintf("\"%s\"", "got erroneous output"))
	} else {
		jsonObject := json.RawMessage(``)
		errr := json.Unmarshal(inputBytes, &jsonObject)
		if errr != nil {
			os.Stderr.WriteString(fmt.Sprintf("unmarshal successful, but bad json conversion: '%s'\n", string(inputBytes)))
			rawMessage = json.RawMessage(fmt.Sprintf("\"%s\"", "got erroneous output"))
		} else {
			rawMessage = jsonObject
		}
	}
	return
}

/*
* all plugin input generation shall take place here
 */
func main() {
	app.Author("Lukas Burgey @ KIT within the INDIGO DataCloud Project")
	app.Version("0.1.7")

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginTest.FullCommand():
		defaultPluginInput.specifyPluginInput(*pluginInputOverride)

		defaultAction = json.RawMessage(fmt.Sprintf("\"%s\"", *pluginTestAction))
		defaultPluginInput["action"] = &defaultAction

		defaultPluginInput.generateUserID()
		defaultPluginInput.validate()

		fmt.Printf("%s", defaultPluginInput.doPluginTest(*pluginTestName))

	case printDefault.FullCommand():
		if *validateDefault {
			defaultPluginInput.validate()
		}

		fmt.Printf("%s", defaultPluginInput.marshalPluginInput())

	case printSpecific.FullCommand():
		defaultPluginInput.specifyPluginInput(*pluginInputOverride)
		defaultPluginInput.generateUserID()
		if *validateSpecific {
			defaultPluginInput.validate()
		}

		fmt.Printf("%s", defaultPluginInput.marshalPluginInput())
	}

	os.Exit(exitCode)
}
