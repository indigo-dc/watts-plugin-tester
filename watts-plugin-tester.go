package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/imdario/mergo"
	"github.com/indigo-dc/watts-plugin-tester/schemes"
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
type pluginOutput interface{}
type pluginOutputJSON map[string]interface{}
type globalOutput map[string](*json.RawMessage)

var (
	exitCode                     = 0
	exitCodePluginError          = 1
	exitCodePluginExecutionError = 2
	exitCodeInternalError        = 3
	exitCodeUserError            = 4

	app          = kingpin.New("watts-plugin-tester", "Test tool for watts plugins")
	pluginAction = app.Flag("plugin-action", "The plugin action to run the plugin with. Defaults to 'parameter'").Short('a').String()
	pluginName   = app.Flag("plugin-name", "Name of the plugin").Short('p').String()

	inputComplementFile   = app.Flag("json-file", "Complement the plugin input with a json file").Short('j').String()
	inputComplementString = app.Flag("json", "Complement the plugin input with a json object (provided as a string)").String()
	inputComplementConf   = app.Flag("config", "Complement the plugin input with the config parameters from a watts config").Short('c').String()
	inputComplementConfID = app.Flag("config-identifier", "Service ID for the watts config").Short('i').String()

	machineReadable        = app.Flag("machine", "Be machine readable (all output will be json)").Short('m').Bool()
	useEnvForParameterPass = app.Flag("env", "Use this environment variable to pass the plugin input to the plugin").Short('e').Bool()
	envVarForParameterPass = app.Flag("env-var", "This environment variable is used to pass the plugin input to the plugin").Default("WATTS_PARAMETER").String()

	pluginCheck = app.Command("check", "Check a plugin against the inbuilt typed schema")

	pluginTest           = app.Command("test", "Test a plugin against the inbuilt typed schema and expected output values")
	expectedOutputFile   = pluginTest.Flag("expected-output-file", "Expected output as a file").String()
	expectedOutputString = pluginTest.Flag("expected-output-string", "Expected output as a string").String()

	printDefault = app.Command("default", "Print the default plugin input as json")

	printSpecific = app.Command("specific", "Print the plugin input (including the user override) as json")

	generateDefault = app.Command("generate", "Generate a fitting json input file for the given plugin")

	outputMessages = []json.RawMessage{}

	// for marshalIndent
	outputIndentation = "                 "
	outputTabWidth    = "    "

	defaultwattVersionString = "1.0.0"
	defaultWattsVersion      = toRawJSONString(defaultwattVersionString)
	defaultCredentialState   = toRawJSONString("undefined")
	defaultConfParams        = json.RawMessage(`{}`)
	defaultParams            = json.RawMessage(`{}`)
	defaultAdditionalLogins  = json.RawMessage(`[]`)
	defaultUserInfo          = json.RawMessage(`{
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
)

func jsonFileToPluginInput(file string) (p pluginInput) {
	checkFileExistence(file)
	overrideBytes, err := ioutil.ReadFile(file)
	check(err, exitCodeUserError, "")
	p = jsonStringToPluginInput(string(overrideBytes))
	return
}

func jsonStringToPluginInput(jsonString string) (p pluginInput) {
	p = pluginInput{}
	err := json.Unmarshal([]byte(jsonString), &p)
	check(err, exitCodeUserError, "on unmarshaling user provided json string")
	return
}

func merge(dest *pluginInput, src *pluginInput) {
	err := mergo.Merge(dest, src)
	check(err, exitCodeInternalError, "merging plugin inputs")
	return
}

func (p *pluginInput) validate() {
	var i interface{}

	bs := marshalIndent(p)
	err := json.Unmarshal(bs, &i)
	check(err, exitCodeInternalError, "unmarshal error")
	path, err := schemes.PluginInputScheme.Validate(i)

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

	j := marshal(userIDJSONReduced)

	escaped := bytes.Replace(j, []byte{'/'}, []byte{'\\', '/'}, -1)
	defaultWattsUserID = toRawJSONString(base64url.Encode(escaped))
	(*p)["watts_userid"] = &defaultWattsUserID
	return
}

func (p *pluginInput) setPluginAction() {
	if *pluginAction != "" {
		validatePluginAction(*pluginAction)
		defaultAction = toRawJSONString(*pluginAction)
		(*p)["action"] = &defaultAction
	} else {
		action := ""
		err := json.Unmarshal(*(*p)["action"], &action)
		check(err, exitCodeInternalError, "setPluginAction")
		validatePluginAction(action)
	}

	return
}

func (p *pluginInput) marshalPluginInput() (s []byte) {
	s = marshalIndent(*p)
	return
}

func (p *pluginInput) specifyPluginInput() {

	// merge a user provided watts config
	if *inputComplementConf != "" {
		checkFileExistence(*inputComplementConf)
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

				confParamsJSON := marshal(confParams)

				defaultConfParams = json.RawMessage(confParamsJSON)
				(*p)["conf_params"] = &defaultConfParams
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

	// merge a user provided json file
	if *inputComplementFile != "" {
		overridePluginInput := jsonFileToPluginInput(*inputComplementFile)
		merge(&overridePluginInput, p)
		*p = overridePluginInput
	}

	// merge a user provided json string
	if *inputComplementString != "" {
		overridePluginInput := jsonStringToPluginInput(*inputComplementString)
		merge(&overridePluginInput, p)
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

	if _, versionFound := schemes.WattsSchemes[string(extractedVersion)]; !versionFound {
		extractedVersion = versionExtractor.Find(defaultWattsVersion)
		(*p)["watts_version"] = &defaultWattsVersion
	}

	version = string(extractedVersion)
	return
}

func (o *globalOutput) executePlugin(pluginName string, p *pluginInput) (po pluginOutput) {
	checkFileExistence(pluginName)
	pluginInputJSON := p.marshalPluginInput()
	inputBase64 := base64.StdEncoding.EncodeToString(pluginInputJSON)

	o.print("plugin_name", pluginName)
	o.printJSON("plugin_input", json.RawMessage(pluginInputJSON))

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

	if err != nil {
		o.print("result", "error")
		o.print("error", fmt.Sprint(err))
		o.printArbitrary("plugin_output", string(outputBytes))
		o.print("description", "error executing the plugin")
		exitCode = 3
		return
	}

	o.printJSON("plugin_output", byteToRawMessage(outputBytes))
	o.print("plugin_time", duration)

	err = json.Unmarshal(outputBytes, &po)
	if err != nil {
		o.print("result", "error")
		o.print("description", "error processing the output of the plugin")
		o.printArbitrary("error", fmt.Sprintf("%s", err))
		exitCode = 1
		return
	}
	return
}

func (o *globalOutput) checkPluginOutput(po pluginOutput, pi *pluginInput) {
	var action, version string

	err := json.Unmarshal(*(*pi)["action"], &action)
	check(err, exitCodeInternalError, "checkPluginOutput")

	version = pi.version()

	path, err := schemes.WattsSchemes[version][action].Validate(po)
	if err != nil {
		o.print("result", "error")
		o.print("description", fmt.Sprintf("validation error %s", err))
		o.print("path", path)
		exitCode = 1
		return
	}

	o.print("result", "ok")
	o.print("description", "validation passed")
	return
}

func (p pluginInput) String() string {
	return fmt.Sprintf("%s", p.marshalPluginInput())
}

func (o *globalOutput) printJSON(a string, b json.RawMessage) {
	/*
		if !*machineReadable {
			bs, err := json.MarshalIndent(&b, outputIndentation, outputTabWidth)
			if err != nil {
				fmt.Printf("%15s: %s\n%15s\n\n", a, string(b), fmt.Sprintf("end of %s", a))
			} else {
				fmt.Printf("%15s: %s\n%15s\n\n", a, string(bs), fmt.Sprintf("end of %s", a))
			}
			return
		}
	*/
	outputMessages = append(outputMessages, b)
	(*o)[a] = &(outputMessages[len(outputMessages)-1])

}

func (o *globalOutput) print(a string, b string) {
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

func (o *globalOutput) testOutputAgainst(po pluginOutput, expectedOutput pluginOutputJSON) {
	bs := marshal(expectedOutput)

	o.printJSON("plugin_output_expected", json.RawMessage(bs))
	poj := po.(pluginOutputJSON)
	for i, v := range expectedOutput {
		if o := poj[i]; o != v {
			app.Errorf("Unexpected output for key %s: '%s' instead of '%s'", i, o, v)
			os.Exit(exitCodePluginError)
		}
	}

	o.print("result", "ok")
	o.print("description", "Test passed. All output as expected")
	fmt.Println(*o)
	return
}

func (o globalOutput) String() (s string) {
	if !*machineReadable {
		var buffer bytes.Buffer
		for i, v := range o {
			buffer.WriteString(fmt.Sprintf("%15s: %s\n", i, *v))
		}
		s = buffer.String()
	} else {
		s = string(marshalIndent(&o))
	}
	return
}

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

func checkFileExistence(name string) {
	_, err := os.Stat(name)
	check(err, exitCodeUserError, "")
}

func validatePluginAction(action string) {
	if action != "request" && action != "parameter" && action != "revoke" {
		app.Errorf("invalid plugin action %s", action)
		os.Exit(exitCodeUserError)
	}
}

func byteToRawMessage(inputBytes []byte) (rawMessage json.RawMessage) {
	rawMessage = json.RawMessage(``)

	testJSONObject := map[string]interface{}{}
	err := json.Unmarshal(inputBytes, &testJSONObject)
	if err != nil {
		rawMessage = toRawJSONString(escapeJSONString(string(inputBytes)))
	} else {
		rawMessage = json.RawMessage(marshalIndent(testJSONObject))
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

func (o *globalOutput) generateConfParams(pluginName string) (confParams json.RawMessage) {
	po := o.executePlugin(pluginName, &defaultPluginInput)
	m := po.(map[string]interface{})
	confParamsInterface := m["conf_params"].([]interface{})

	generatedConfig := map[string](interface{}){}
	for _, v := range confParamsInterface {
		mm := v.(map[string]interface{})
		generatedConfig[mm["name"].(string)] = mm["default"].(string)
	}

	b := marshal(generatedConfig)
	return byteToRawMessage(b)
}

func jsonFileToMap(file string) (m pluginOutputJSON) {
	checkFileExistence(file)
	overrideBytes, err := ioutil.ReadFile(file)
	check(err, exitCodeUserError, "")
	m = jsonStringToMap(string(overrideBytes))
	return
}

func jsonStringToMap(jsonString string) (m pluginOutputJSON) {
	m = pluginOutputJSON{}
	err := json.Unmarshal([]byte(jsonString), &m)
	check(err, exitCodeUserError, "on unmarshaling user provided json string")
	return
}

func getExpectedOutput() (m pluginOutputJSON) {
	if *expectedOutputFile != "" {
		m = jsonFileToMap(*expectedOutputFile)
	} else if *expectedOutputString != "" {
		m = jsonStringToMap(*expectedOutputString)
	} else {
		app.Errorf("No expected output provided")
		os.Exit(exitCodeUserError)
	}
	return
}

func marshal(i interface{}) (bytes []byte) {
	bytes, err := json.Marshal(i)
	check(err, exitCodeInternalError, "marshal")
	return
}

func marshalIndent(i interface{}) (bytes []byte) {
	indentation := ""
	if !*machineReadable {
		indentation = outputIndentation
	}

	bytes, err := json.MarshalIndent(i, indentation, outputTabWidth)
	check(err, exitCodeInternalError, "marshalIndent")
	return bytes
}

func main() {
	app.Author("Lukas Burgey @ KIT within the INDIGO DataCloud Project")
	app.Version("1.0.0")

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginCheck.FullCommand():
		defaultPluginInput.specifyPluginInput()
		g := globalOutput{}
		po := g.executePlugin(*pluginName, &defaultPluginInput)
		g.checkPluginOutput(po, &defaultPluginInput)
		fmt.Printf("%s", g)

	case pluginTest.FullCommand():
		*machineReadable = true
		eo := getExpectedOutput()
		defaultPluginInput.specifyPluginInput()
			
		g := globalOutput{}
		po := g.executePlugin(*pluginName, &defaultPluginInput)
		g.checkPluginOutput(po, &defaultPluginInput)
		g.testOutputAgainst(po, eo)

		fmt.Printf("%s", g)

	case generateDefault.FullCommand():
		*machineReadable = true
		defaultPluginInput.specifyPluginInput()
		g := globalOutput{}
		defaultConfParams = g.generateConfParams(*pluginName)
		defaultPluginInput.validate()
		fmt.Printf("%s", defaultPluginInput)

	case printDefault.FullCommand():
		*machineReadable = true
		fmt.Printf("%s", defaultPluginInput)

	case printSpecific.FullCommand():
		*machineReadable = true
		defaultPluginInput.specifyPluginInput()
		fmt.Printf("%s", defaultPluginInput)
	}

	os.Exit(exitCode)
}
