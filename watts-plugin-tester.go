package main

import (
	"os"
	"os/exec"
	"fmt"
	"gopkg.in/alecthomas/kingpin.v2"
	"encoding/base64"
	"encoding/json"
	v "github.com/gima/govalid/v1"
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


var (
	app = kingpin.New("watts-plugin-tester", "usage message")
	pluginTest = app.Command("test", "test plugin")
	pluginTestName = pluginTest.Arg("pluginName", "Name of the plugin to test").Required().String()
	pluginTestAction = pluginTest.Arg("pluginAction", "The plugin action to be tested. If not supplied all actions will be tested").String()

	actions = []string{"parameter"}
	userId = "max_mustermann"
	pluginInput = PluginInput{
		WattsVersion: "1.0",
		ConfParams: "{}",
		Params: "{}",
		CredState: "undefined",
		UserInformation: UserInfo{
			FamilyName: "Mustermann",
			Gender: "Male",
			GivenName: "Max",
			ISS: "https://issuer.example.com",
			Name: "Max Mustermann",
			Sub: "123456789",
		},
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
	}
)

func validateAction(action string, pluginOutput interface{}) {
}

func doPluginTest(pluginName string) {
	fmt.Println("testing plugin ", pluginName)

	for _, action := range actions {
		doPluginTestAction(pluginName, action)
	}
}

func doPluginTestAction(pluginName string, actionName string) (result string) {
	fmt.Println("testing ", pluginName, "->", actionName)

	pluginInput.Action = actionName
	pluginInput.WattsUserid = base64.StdEncoding.EncodeToString([]byte(userId))

	inputJson, _ := json.Marshal(pluginInput)
	inputBase64 := base64.StdEncoding.EncodeToString([]byte(inputJson))

	out, err := exec.Command(pluginName, inputBase64).Output()
	if err != nil {
		fmt.Println("Error executing command: ", err)
		result = "error"
		return
	}

	fmt.Println("Output: ", string(out))

	var pluginOutput interface{}
	json.Unmarshal(out, &pluginOutput)

	path, errr := schemes[actionName].Validate(pluginOutput)
	if errr == nil {
		fmt.Println("Validation passed")
	} else {
		fmt.Printf("Validation error at %s. Error (%s)", path, errr)
	}

	return
}

func main() {
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginTest.FullCommand():
		doPluginTest(*pluginTestName)
	}
}
