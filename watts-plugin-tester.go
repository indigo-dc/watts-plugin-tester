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

type jsonObject map[string]interface{}
type pluginOutput interface{}

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

	// for marshalIndent
	outputIndentation = "                 "
	outputTabWidth    = "    "

	defaultwattVersionString = "1.0.0"
	defaultPluginInput = jsonObject{
		"action": "parameter",
		"watts_version":     "1.0.0",
		"cred_state":        "undefined",
		"conf_params":       map[string]interface{}{},
		"params":            map[string]interface{}{},
		"user_info":         map[string]interface{}{
			"iss": "https://issuer.example.com",
			"sub": "123456789",
		},
		"additional_logins": []interface{}{},
	}
)

func jsonFileToObject(file string) (m jsonObject) {
	checkFileExistence(file)
	overrideBytes, err := ioutil.ReadFile(file)
	check(err, exitCodeUserError, "")
	m = jsonStringToObject(string(overrideBytes))
	return
}

func jsonStringToObject(jsonString string) (m jsonObject) {
	err := json.Unmarshal([]byte(jsonString), &m)
	check(err, exitCodeUserError, "on unmarshaling user provided json string")
	return
}

func merge(dest *jsonObject, src *jsonObject) {
	err := mergo.Merge(dest, src)
	check(err, exitCodeInternalError, "merging plugin inputs")
	return
}

func (p *jsonObject) validate() {
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

func (p *jsonObject) generateUserID() {
	userIDJSONReduced := map[string]interface{}{}

	userInfo := (*p)["user_info"].(map[string]interface{})
	userIDJSONReduced["issuer"] = userInfo["iss"]
	userIDJSONReduced["subject"] = userInfo["sub"]

	j := marshal(userIDJSONReduced)

	escaped := bytes.Replace(j, []byte{'/'}, []byte{'\\', '/'}, -1)
	(*p)["watts_userid"] = base64url.Encode(escaped)
	return
}

func (p *jsonObject) setPluginAction() {
	if *pluginAction != "" {
		validatePluginAction(*pluginAction)
		defaultAction := toRawJSONString(*pluginAction)
		(*p)["action"] = &defaultAction
	} else {
		action := (*p)["action"].(string)
		validatePluginAction(action)
	}

	return
}

func (p *jsonObject) marshalPluginInput() (s []byte) {
	s = marshalIndent(*p)
	return
}

func (p *jsonObject) specifyPluginInput() {

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

				defaultConfParams := json.RawMessage(confParamsJSON)
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
		overridePluginInput := jsonFileToObject(*inputComplementFile)
		merge(&overridePluginInput, p)
		*p = overridePluginInput
	}

	// merge a user provided json string
	if *inputComplementString != "" {
		overridePluginInput := jsonStringToObject(*inputComplementString)
		merge(&overridePluginInput, p)
		*p = overridePluginInput
	}

	p.generateUserID()
	p.setPluginAction()
	p.validate()
}

func (p *jsonObject) version() (version string) {
	versionJSON := (*p)["watts_version"]
	versionBytes, err := json.Marshal(&versionJSON)
	check(err, exitCodeInternalError, "")

	versionExtractor, _ := regexp.Compile("[^\"+v]+")
	extractedVersion := versionExtractor.Find(versionBytes)

	if _, versionFound := schemes.WattsSchemes[string(extractedVersion)]; !versionFound {
		extractedVersion = versionExtractor.Find((*p)["watts_version"].([]byte))
		(*p)["watts_version"] = defaultwattVersionString
	}

	version = string(extractedVersion)
	return
}

func (o *jsonObject) executePlugin(pluginName string, p *jsonObject) (po pluginOutput) {
	checkFileExistence(pluginName)
	jsonObjectJSON := p.marshalPluginInput()
	inputBase64 := base64.StdEncoding.EncodeToString(jsonObjectJSON)

	o.print("plugin_name", pluginName)
	o.print("plugin_input", p)

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
		o.print("plugin_output", string(outputBytes))
		o.print("description", "error executing the plugin")
		exitCode = 3
		return
	}

	o.print("plugin_time", duration)

	err = json.Unmarshal(outputBytes, &po)
	if err != nil {
		o.print("result", "error")
		o.print("description", "error processing the output of the plugin")
		o.print("error", fmt.Sprintf("%s", err))
		exitCode = 1
		return
	}
	o.print("plugin_output", po)
	return
}

func (o *jsonObject) checkPluginOutput(po pluginOutput, pi *jsonObject) {
	version := pi.version()
	action := (*pi)["action"].(string)

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

func (o *jsonObject) print(a string, b interface{}) {
	(*o)[a] = b
}

func (o *jsonObject) testOutputAgainst(po pluginOutput, expectedOutput jsonObject) {
	o.print("plugin_output_expected", expectedOutput)
	poj := po.(jsonObject)
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

func (o jsonObject) String() (s string) {
	if !*machineReadable {
		var buffer bytes.Buffer
		for i, v := range(o) {
			buffer.WriteString(fmt.Sprintf("%15s: %s\n", i, string(marshalIndent(v))))
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

func (o *jsonObject) generateConfParams(pluginName string) (confParams map[string]interface{}) {
	po := o.executePlugin(pluginName, &defaultPluginInput)
	m := po.(map[string]interface{})
	confParamsInterface := m["conf_params"].([]interface{})

	generatedConfig := map[string]interface{}{}
	for _, v := range confParamsInterface {
		mm := v.(map[string]interface{})
		generatedConfig[mm["name"].(string)] = mm["default"].(string)
	}
	return
}


func getExpectedOutput() (m jsonObject) {
	if *expectedOutputFile != "" {
		m = jsonFileToObject(*expectedOutputFile)
	} else if *expectedOutputString != "" {
		m = jsonStringToObject(*expectedOutputString)
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
	globalOutput := jsonObject{}

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginCheck.FullCommand():
		defaultPluginInput.specifyPluginInput()
		po := globalOutput.executePlugin(*pluginName, &defaultPluginInput)
		globalOutput.checkPluginOutput(po, &defaultPluginInput)

	case pluginTest.FullCommand():
		*machineReadable = true
		eo := getExpectedOutput()
		defaultPluginInput.specifyPluginInput()
			
		po := globalOutput.executePlugin(*pluginName, &defaultPluginInput)
		globalOutput.checkPluginOutput(po, &defaultPluginInput)
		globalOutput.testOutputAgainst(po, eo)

	case generateDefault.FullCommand():
		*machineReadable = true
		defaultPluginInput.specifyPluginInput()
		g := jsonObject{}
		defaultPluginInput["conf_params"] = g.generateConfParams(*pluginName)
		defaultPluginInput.validate()
		fmt.Printf("%s", defaultPluginInput)

	case printDefault.FullCommand():
		*machineReadable = true
		globalOutput = defaultPluginInput

	case printSpecific.FullCommand():
		*machineReadable = true
		defaultPluginInput.specifyPluginInput()
		globalOutput = defaultPluginInput
	}
	fmt.Printf("%s", globalOutput)

	os.Exit(exitCode)
}
