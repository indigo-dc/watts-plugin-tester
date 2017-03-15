package main

import (
	"os"
	"os/exec"
	"fmt"
	"gopkg.in/alecthomas/kingpin.v2"
	"encoding/base64"
	"encoding/json"
	v "github.com/gima/govalid/v1"
	"io/ioutil"
	"github.com/imdario/mergo"
)

type UserInfo struct {
	FamilyName string `json:"family_name"`
	Gender string `json:"gender"`
	GivenName string`json:"given_name"`
	ISS string `json:"iss"`
	Name string `json:"name"`
	Sub string `json:"sub"`
}

type PluginInput struct {
	WattsVersion string `json:"watts_version"`
	Action string `json:"action"`
	ConfParams string `json:"conf_params"`
	Params string `json:"params"`
	CredState string `json:"cred_state"`
	UserInformation UserInfo `json:"user_info"`
	WattsUserid string `json:"watts_userid"`
}

type User struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

type Output struct{m map[string]string}

var (
	app = kingpin.New("watts-plugin-tester", "Test tool for watts plugins")
	pluginTestAction = app.Flag("plugin-action", "The plugin action to be tested. Defaults to \"parameter\"").Default("parameter").Short('a').String()
	pluginInputOverride = app.Flag("json", "Use user provided json to override the inbuilt one").Short('j').String()
	//verbose = app.Flag("verbose", "Be verbose").Short('v').Bool()
	machineReadable = app.Flag("machine", "Be machine readable (all output will be json)").Short('m').Bool()

	pluginTest = app.Command("test", "Test a plugin")
	pluginTestName = pluginTest.Arg("pluginName", "Name of the plugin to test").Required().String()

	printDefault = app.Command("default", "Print the default plugin input as json")
	printSpecific = app.Command("specific", "Print the plugin input (including the user override) as json")

	defaultUserInfo = UserInfo{
		FamilyName: "Mustermann",
		Gender: "Male",
		GivenName: "Max",
		ISS: "https://issuer.example.com",
		Name: "Max Mustermann",
		Sub: "123456789",
	}
	defaultPluginInput = PluginInput{
		WattsVersion: "1.0",
		ConfParams: "{}",
		Params: "{}",
		CredState: "undefined",
		UserInformation: defaultUserInfo ,
	}

	schemes =  map[string]v.Validator{
		"parameter": v.Object(
			v.ObjKV("result", v.String(v.StrIs("ok"))),
			v.ObjKV("conf_params", v.Array(v.ArrEach(
				v.Object(
					v.ObjKV("name", v.String()),
					v.ObjKV("type", v.String()),
					v.ObjKV("default", v.String()),
				),
			))),
			v.ObjKV("request_params", v.Array(v.ArrEach(
				v.Array(v.ArrEach(
					v.Object(
						v.ObjKV("key", v.String()),
						v.ObjKV("name", v.String()),
						v.ObjKV("description", v.String()),
						v.ObjKV("type", v.String()),
						v.ObjKV("mandatory", v.Boolean()),
					),
				)),
			))),
			v.ObjKV("version", v.String()),
		),
		"request": v.Or(
			v.Object(
				v.ObjKV("result", v.String(v.StrIs("ok"))),
				v.ObjKV("credential", v.Array(v.ArrEach(
					v.Object(
						v.ObjKV("name", v.String()),
						v.ObjKV("type", v.String()),
						v.ObjKV("value", v.String()),
					),
				))),
				v.ObjKV("state", v.String()),
			),
			v.Object(
				v.ObjKV("result", v.String(v.StrIs("error"))),
				v.ObjKV("user_msg", v.String()),
				v.ObjKV("log_msg", v.String()),
			),
		),
	}
)

func (p *PluginInput) generateUserID() {
	userIdJson := map[string]string{
		"issuer": p.UserInformation.ISS,
		"subject": p.UserInformation.Sub,
	}
	j, _ := json.Marshal(userIdJson)
	p.WattsUserid= base64.RawStdEncoding.EncodeToString([]byte(j))
	return
}

func marshalPluginInput(p PluginInput) (s []byte) {
	s, _ = json.MarshalIndent(p, "", "    ")
	return
}

func specificJson(p PluginInput) (pi PluginInput) {
	if *pluginInputOverride != "" {
		inputOverride, err := ioutil.ReadFile(*pluginInputOverride)
		if err !=  nil {
			// TODO machine readability
			fmt.Println("Error reading file ", *pluginInputOverride, " (", err, ")")
			return
		}

		errr := json.Unmarshal(inputOverride, &pi)
		if errr != nil {
			return
		}

		mergo.Merge(&pi, p)
	} else {
		pi  = p
	}

	pi.generateUserID()
	return
}

func doPluginTest(pluginName string) (output Output) {
	output.m = map[string]string{}

	output.print("plugin_name", pluginName)
	output.print("action", *pluginTestAction)

	pi := specificJson(defaultPluginInput)
	pi.Action = *pluginTestAction
	inputBase64 := base64.StdEncoding.EncodeToString(marshalPluginInput(pi))

	out, err := exec.Command(pluginName, inputBase64).Output()
	if err != nil {
		output.print("result", "error")
		output.print("description", "error executing the plugin")
		return
	}

	output.print("plugin_output", string(out))

	var pluginOutput interface{}
	json.Unmarshal(out, &pluginOutput)

	path, errr := schemes[*pluginTestAction].Validate(pluginOutput)
	if errr != nil {
		output.print("result", "error")
		output.print("description", fmt.Sprintf("Validation error at %s. Error (%s)", path, errr))
		return
	}

	output.print("result", "ok")
	output.print("description", "validation passed")
	return
}

func (o *Output) print(identifier string, output string) {
	o.m[identifier] = output

	if !*machineReadable {
		fmt.Printf("%15s: %s\n", identifier, output)
	}
}

func printMachineReadable(o Output) (bs []byte) {
	if *machineReadable {
		bs, _ = json.MarshalIndent(o.m, "", "    ")
	}
	return
}


func main() {
	var output []byte

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginTest.FullCommand():
		o := doPluginTest(*pluginTestName)
		output = printMachineReadable(o)
	case printDefault.FullCommand():
		output = marshalPluginInput(defaultPluginInput)
	case printSpecific.FullCommand():
		output = marshalPluginInput(specificJson(defaultPluginInput))
	}

	fmt.Printf("%s", string(output))
}
