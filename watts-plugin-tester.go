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
	"time"
)

type jsonObject map[string]interface{}

var (
	exitCode                     = 0
	exitCodePluginError          = 1
	exitCodePluginExecutionError = 2
	exitCodeInternalError        = 3
	exitCodeUserError            = 4

	app          = kingpin.New("watts-plugin-tester", "Test tool for watts plugins")
	pluginAction = app.Flag("plugin-action", "The plugin action to run the plugin with. Defaults to 'parameter'").Short('a').String()
	pluginName   = app.Flag("plugin", "Name of the plugin").Short('p').String()

	inputComplementFile   = app.Flag("input-file", "Complement the plugin input with a json file").Short('j').String()
	inputComplementString = app.Flag("input-string", "Complement the plugin input with a json object (provided as a string)").String()
	inputComplementConf   = app.Flag("input-config", "Complement the plugin input with the config parameters from a watts config").Short('c').String()
	inputComplementConfID = app.Flag("input-config-identifier", "Service ID for the watts config").Short('i').String()

	machineReadable        = app.Flag("machine", "Be machine readable (all output will be json)").Short('m').Bool()
	useEnvForParameterPass = app.Flag("env", "Use this environment variable to pass the plugin input to the plugin").Short('e').Bool()
	envVarForParameterPass = app.Flag("env-var", "This environment variable is used to pass the plugin input to the plugin").Default("WATTS_PARAMETER").String()

	pluginCheck = app.Command("check", "Check a plugin against the inbuilt typed schema")

	pluginTest           = app.Command("test", "Test a plugin against the inbuilt typed schema and expected output values. Provide an expected json")
	expectedOutputFile   = pluginTest.Flag("expected-output-file", "Expected output as a file").String()
	expectedOutputString = pluginTest.Flag("expected-output-string", "Expected output as a string").String()

	pluginTests       = app.Command("tests", "Test a plugin using test config")
	pluginTestsConfig = pluginTests.Arg("config", "Config file for the tests to run").Required().String()

	printDefault = app.Command("default", "Print the default plugin input as json")

	printSpecific = app.Command("specific", "Print the plugin input (including the user override) as json")

	generateDefault = app.Command("generate", "Generate a fitting json input file for the given plugin")

	// for marshalIndent
	outputIndentation = "                 "
	outputTabWidth    = "    "

	defaultWattsVersion = "1.0.0"
	defaultPluginInput  = jsonObject{
		"action":        "parameter",
		"watts_version": defaultWattsVersion,
		"cred_state":    "undefined",
		"conf_params":   map[string]interface{}{},
		"params":        map[string]interface{}{},
		"user_info": map[string]interface{}{
			"iss": "https://issuer.example.com",
			"sub": "123456789",
		},
		"additional_logins": []interface{}{},
	}
)

// helpers
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

func typeAssertMap(i interface{}) (m map[string]interface{}) {
	m, ok := i.(map[string]interface{})
	if !ok {
		app.Errorf("Type Assertion: %s was type %T not %T", i, i, m)
		os.Exit(exitCodeInternalError)
	}
	return
}

func typeAssertString(i interface{}) (s string) {
	s, ok := i.(string)
	if !ok {
		app.Errorf("Type Assertion: %s was type %T not %T", i, i, s)
		os.Exit(exitCodeInternalError)
	}
	return
}

func typeAssertList(i interface{}) (l []interface{}) {
	l, ok := i.([]interface{})
	if !ok {
		app.Errorf("Type Assertion: %s was type %T not %T", i, i, l)
		os.Exit(exitCodeInternalError)
	}
	return
}

func checkFileExistence(name string) {
	_, err := os.Stat(name)
	check(err, exitCodeUserError, "")
}

func jsonFileToObject(file string) jsonObject {
	checkFileExistence(file)
	overrideBytes, err := ioutil.ReadFile(file)
	check(err, exitCodeUserError, "on reading user provided json file")
	return jsonStringToObject(string(overrideBytes))
}

func jsonStringToObject(jsonString string) (m jsonObject) {
	err := json.Unmarshal([]byte(jsonString), &m)
	check(err, exitCodeUserError, "on unmarshaling user provided json string")
	return
}

func marshal(i interface{}) (bs[]byte) {
	b := new(bytes.Buffer)
	encoder := json.NewEncoder(b)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(i)
	check(err, exitCodeInternalError, "marshal")
	bs = b.Bytes()
	return
}

func marshalIndent(i interface{}) (bs []byte) {
	indentation := ""
	if !*machineReadable {
		indentation = outputIndentation
	}

	b := new(bytes.Buffer)
	encoder := json.NewEncoder(b)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent(indentation, outputTabWidth)
	err := encoder.Encode(i)
	check(err, exitCodeInternalError, "marshalIndent")
	bs = b.Bytes()
	return
}

func (o *jsonObject) print(a string, b interface{}) {
	(*o)[a] = b
}

func printGlobalOutput(globalOutput jsonObject) {
	s := ""
	if !*machineReadable {
		var buffer bytes.Buffer
		for i, v := range globalOutput {
			buffer.WriteString(fmt.Sprintf("%15s: %s\n", i, string(marshalIndent(v))))
		}
		s = buffer.String()
	} else {
		s = string(marshalIndent(globalOutput))
	}
	fmt.Printf("%s", s)
	return
}

// pluginInput processing
func validate(pluginInput jsonObject) {
	path, err := schemes.PluginInputScheme.Validate(pluginInput)
	check(err, exitCodePluginError, fmt.Sprintf("on validating plugin input at path %s", path))
	return
}

func validatePluginAction(action string) {
	if action != "request" && action != "parameter" && action != "revoke" {
		app.Errorf("invalid plugin action %s", action)
		os.Exit(exitCodeUserError)
	}
}

func generateUserID(pluginInput jsonObject) jsonObject {
	userInfo := typeAssertMap(pluginInput["user_info"])
	j := marshal(jsonObject{
		"issuer": userInfo["iss"],
		"subject": userInfo["sub"],
	})

	escaped := bytes.Replace(j, []byte{'/'}, []byte{'\\', '/'}, -1)
	pluginInput["watts_userid"] = base64url.Encode(escaped)
	return pluginInput
}

func setPluginAction(pluginInput jsonObject) jsonObject {
	if *pluginAction != "" {
		validatePluginAction(*pluginAction)
		pluginInput["action"] = *pluginAction
	} else {
		action := pluginInput["action"].(string)
		validatePluginAction(action)
	}
	return pluginInput
}

func marshalPluginInput(pluginInput jsonObject) (s []byte) {
	s = marshalIndent(pluginInput)
	return
}

func pluginInputFromConf(file string, identifier string) jsonObject {
	if file != "" {
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
				return jsonObject{"conf_params": confParams}
			}

			app.Errorf("Could not find configuration parameters for '%s' in '%s'",
			*inputComplementConfID, *inputComplementConf)
			os.Exit(exitCodeUserError)
		} else {
			app.Errorf("Need a config identifier for config override")
			os.Exit(exitCodeUserError)
		}
	}
	return jsonObject{}
}

func specifyPluginInput(pluginInput jsonObject) (specificPluginInput jsonObject) {
	specificPluginInput = defaultPluginInput

	// merge a user provided watts config
	err := mergo.MergeWithOverwrite(&specificPluginInput,
					pluginInputFromConf(*inputComplementFile, *inputComplementConfID))
	check(err, exitCodeInternalError, "merging plugin input from conf")

	
	// merge the given base input
	err = mergo.MergeWithOverwrite(&specificPluginInput, pluginInput)
	check(err, exitCodeInternalError, "merging plugin input")

	// merge a user provided json file
	if *inputComplementFile != "" {
		err = mergo.MergeWithOverwrite(&specificPluginInput, jsonFileToObject(*inputComplementFile))
		check(err, exitCodeInternalError, "merging plugin input from complement file")
	}

	// merge a user provided json string
	if *inputComplementString != "" {
		err = mergo.MergeWithOverwrite(&specificPluginInput, jsonStringToObject(*inputComplementString))
		check(err, exitCodeInternalError, "merging plugin input from complement string")
	}

	specificPluginInput = setPluginAction(specificPluginInput)
	specificPluginInput = generateUserID(specificPluginInput)
	validate(specificPluginInput)
	return
}

func version(pluginInput jsonObject) (version string) {
	versionJSON := pluginInput["watts_version"]
	versionBytes, err := json.Marshal(&versionJSON)
	check(err, exitCodeInternalError, "")

	versionExtractor, _ := regexp.Compile("[^\"+v]+")
	extractedVersion := versionExtractor.Find(versionBytes)

	if _, versionFound := schemes.WattsSchemes[string(extractedVersion)]; !versionFound {
		extractedVersion = versionExtractor.Find(pluginInput["watts_version"].([]byte))
		pluginInput["watts_version"] = defaultWattsVersion
	}

	version = string(extractedVersion)
	return
}

func getExpectedOutput() (expectedOutput jsonObject) {
	if *expectedOutputFile != "" {
		expectedOutput = jsonFileToObject(*expectedOutputFile)
	} else if *expectedOutputString != "" {
		expectedOutput = jsonStringToObject(*expectedOutputString)
	} else {
		app.Errorf("No expected output provided")
		os.Exit(exitCodeUserError)
	}
	return
}

func (o *jsonObject) terminate(exitCode int) {
	printGlobalOutput(*o)
	os.Exit(exitCode)
}

// plugin execution
func (o *jsonObject) executePlugin(pluginName string, pluginInput jsonObject) (pluginOutput interface{}) {
	checkFileExistence(pluginName)
	inputBase64 := base64.StdEncoding.EncodeToString(marshalPluginInput(pluginInput))

	plugin := jsonObject{}
	plugin.print("name", pluginName)
	plugin.print("input", pluginInput)

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
	plugin.print("duration", duration)

	if err != nil {
		plugin.print("result", "error")
		plugin.print("error", fmt.Sprint(err))
		plugin.print("description", "error executing the plugin")

		plugin.print("output", outputBytes)
		o.print("plugin", plugin)
		o.terminate(exitCodePluginExecutionError)
	}

	err = json.Unmarshal(outputBytes, &pluginOutput)
	if err != nil {
		plugin.print("result", "error")
		plugin.print("error", fmt.Sprint(err))
		plugin.print("description", "Error processing the output of the plugin")

		plugin.print("output", outputBytes)
		o.print("plugin", plugin)
		o.terminate(exitCodeInternalError)
	}

	plugin.print("output", pluginOutput)
	o.print("plugin", plugin)
	return
}

func (o *jsonObject) checkPluginOutput(pluginOutput interface{}, pluginInput jsonObject) bool {
	version := version(pluginInput)
	action := pluginInput["action"].(string)

	path, err := schemes.WattsSchemes[version][action].Validate(pluginOutput)
	if err != nil {
		o.print("result", "error")
		o.print("description", fmt.Sprintf("Validation error %s at %s", err, path))
		o.print("validation_error", map[string]interface{}{"error": fmt.Sprint(err), "path": path})
		return false
	}

	o.print("result", "ok")
	o.print("description", "Validation passed")
	return true
}

func (o *jsonObject) testPluginOutput(pluginOutput interface{}, pluginInput jsonObject, expectedOutput jsonObject) bool {
	if !o.checkPluginOutput(pluginOutput, pluginInput) {
		return false
	}

	plugin := (*o)["plugin"].(jsonObject)
	plugin.print("output_expected", expectedOutput)

	po := typeAssertMap(pluginOutput)
	for i, expectedValue := range expectedOutput {
		realValue := po[i]
		equals := false

		switch expectedValue.(type) {
		case []interface{}:
			switch realValue.(type) {
			case []interface{}:
				equals = true
			default:
				equals = false
			}
		default:
			equals = realValue == expectedValue
		}

		if !equals {
			o.print("result", "error")
			o.print("description", fmt.Sprintf(
				"Unexpected output for key %s: '%s' instead of '%s'", i, realValue, expectedOutput))
			return false
		}
	}

	o.print("result", "ok")
	o.print("description", "Test passed. All output as expected")
	return true
}

func (o *jsonObject) generateConfParams(pluginName string, pluginInput jsonObject) jsonObject {
	rawOutput := o.executePlugin(pluginName, pluginInput)
	if !o.checkPluginOutput(rawOutput, pluginInput) {
		o.terminate(exitCodePluginError)
	}

	pluginOutput := typeAssertMap(rawOutput)
	confParamsList := typeAssertList(pluginOutput["conf_params"])

	confParams := jsonObject{}
	for _, v := range confParamsList {
		m := typeAssertMap(v)
		confParams[typeAssertString(m["name"])] = m["default"]
	}
	pluginInput["conf_params"] = confParams
	return pluginInput
}

func (o *jsonObject) runTests(config jsonObject) bool {
	pluginName := typeAssertString(config["exec_file"])
	tests := typeAssertList(config["tests"])
	testPassedList := []jsonObject{}
	testFailedList := []jsonObject{}
	testResult := map[string]int{"total": 0, "passed": 0, "failed": 0}

	for _, t := range tests {
		testResult["total"]++

		testOutput := jsonObject{}
		test := typeAssertMap(t)
		pi := jsonObject(typeAssertMap(test["input"]))
		spi := specifyPluginInput(pi)
		eo := jsonObject(typeAssertMap(test["expected_output"]))
		po := testOutput.executePlugin(pluginName, pi)

		if testOutput.testPluginOutput(po, spi, eo) {
			testResult["passed"]++
			testPassedList = append(testPassedList, testOutput)
		} else {
			testResult["failed"]++
			testFailedList = append(testFailedList, testOutput)
		}
	}
	o.print("tests", map[string][]jsonObject{
		"passed": testPassedList,
		"failed": testFailedList,
	})
	o.print("result", "ok")
	o.print("stats", testResult)

	if testResult["failed"] > 0 {
		return false
	}
	return true
}

// main
func main() {
	app.Author("Lukas Burgey @ KIT within the INDIGO DataCloud Project")
	app.Version("3.0.3")
	globalOutput := jsonObject{}

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginCheck.FullCommand():
		pi := specifyPluginInput(defaultPluginInput)
		po := globalOutput.executePlugin(*pluginName, pi)
		if !globalOutput.checkPluginOutput(po, pi) {
			exitCode = exitCodePluginError
		}

	case pluginTest.FullCommand():
		pi := specifyPluginInput(defaultPluginInput)
		po := globalOutput.executePlugin(*pluginName, pi)
		eo := getExpectedOutput()
		if !globalOutput.testPluginOutput(po, pi, eo) {
			exitCode = exitCodePluginError
		}

	case pluginTests.FullCommand():
		config := jsonFileToObject(*pluginTestsConfig)
		if !globalOutput.runTests(config) {
			exitCode = exitCodePluginError
		}

	case generateDefault.FullCommand():
		*machineReadable = true
		pi := specifyPluginInput(defaultPluginInput)
		gpi := globalOutput.generateConfParams(*pluginName, pi)
		validate(gpi)
		globalOutput = gpi

	case printDefault.FullCommand():
		*machineReadable = true
		globalOutput = defaultPluginInput

	case printSpecific.FullCommand():
		*machineReadable = true
		globalOutput = specifyPluginInput(defaultPluginInput)
	}

	printGlobalOutput(globalOutput)
	os.Exit(exitCode)
}
