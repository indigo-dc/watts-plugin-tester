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
)

type PluginInput struct {
	WattsVersion    string           `json:"watts_version"`
	Action          string           `json:"action"`
	ConfParams      *json.RawMessage `json:"conf_params"`
	Params          *json.RawMessage `json:"params"`
	CredState       string           `json:"cred_state"`
	UserInformation *json.RawMessage `json:"user_info"`
	WattsUserid     string           `json:"watts_userid"`
}

type User struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

type Output struct {
	M map[string]string `json:"meta"`
	O json.RawMessage   `json:"output"`
}

type ErrorOutput struct {
	Meta          map[string]string `json:"meta"`
	ErrorString   string            `json:"error"`
	InvalidOutput string            `json:"invalid_output"`
}

var (
	version = "0.0.1"

	app                 = kingpin.New("watts-plugin-tester", "Test tool for watts plugins")
	pluginTestAction    = app.Flag("plugin-action", "The plugin action to be tested. Defaults to \"parameter\"").Default("parameter").Short('a').String()
	printVersion        = app.Command("version", "Print the version information")
	pluginInputOverride = app.Flag("json", "Use user provided json to override the inbuilt one").Short('j').String()
	//verbose = app.Flag("verbose", "Be verbose").Short('v').Bool()
	machineReadable = app.Flag("machine", "Be machine readable (all output will be json)").Short('m').Bool()

	pluginTest     = app.Command("test", "Test a plugin")
	pluginTestName = pluginTest.Arg("pluginName", "Name of the plugin to test").Required().String()

	printDefault  = app.Command("default", "Print the default plugin input as json")
	printSpecific = app.Command("specific", "Print the plugin input (including the user override) as json")

	defaultConfParams = json.RawMessage(`{}`)
	defaultParams     = json.RawMessage(`{}`)
	defaultUserInfo   = json.RawMessage(`{
		"iss": "https://issuer.example.com",
		"sub": "123456789"
	}`)

	defaultPluginInput = PluginInput{
		WattsVersion:    "1.0.0",
		ConfParams:      &defaultConfParams,
		Params:          &defaultParams,
		UserInformation: &defaultUserInfo,
		CredState:       "undefined",
	}

	pluginInputScheme = v.Object(
		v.ObjKV("watts_version", v.String()),
		v.ObjKV("cred_state", v.String()),
		v.ObjKV("conf_params", v.Object()),
		v.ObjKV("params", v.Object()),
		v.ObjKV("user_info",
			v.Object(
				v.ObjKV("iss", v.String()),
				v.ObjKV("sub", v.String()),
			),
		),
	)

	schemes = map[string]v.Validator{
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
		"revoke": v.Or(
			v.Object(
				v.ObjKV("result", v.String(v.StrIs("ok"))),
			),
			v.Object(
				v.ObjKV("result", v.String(v.StrIs("error"))),
				v.ObjKV("user_msg", v.String()),
				v.ObjKV("log_msg", v.String()),
			),
		),
	}
)

func (p *PluginInput) validate() {
	var bs []byte
	var i interface{}

	bs, _ =  json.Marshal(*p)
	json.Unmarshal(bs, &i)
	path, err := pluginInputScheme.Validate(i)
	
	if err != nil {
		fmt.Printf("Validation error at %s. Error (%s)\n%s", path, err, bs)
		os.Exit(1)
	}


	return
}

func (p *PluginInput) generateUserID() {
	userIdJson := map[string]string{
		/*
		"issuer":  p.UserInformation.ISS,
		"subject": p.UserInformation.Sub,
		*/
		"issuer":  "foo issuer",
		"subject": "foo subject",
	}
	j, _ := json.Marshal(userIdJson)
	escaped := bytes.Replace(j, []byte{'/'}, []byte{'\\', '/'}, -1)
	p.WattsUserid = base64url.Encode(escaped)
	return
}

func marshalPluginInput(p PluginInput) (s []byte) {
	s, _ = json.MarshalIndent(p, "", "    ")
	return
}

func specificJson(p PluginInput) (pi PluginInput) {
	if *pluginInputOverride != "" {
		inputOverride, err := ioutil.ReadFile(*pluginInputOverride)
		if err != nil {
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
		pi = p
	}

	pi.generateUserID()
	pi.validate()
	return
}

func doPluginTest(pluginName string) (output Output) {
	output.M = map[string]string{}

	output.print("plugin_name", pluginName)
	output.print("action", *pluginTestAction)

	pi := specificJson(defaultPluginInput)
	pi.Action = *pluginTestAction
	inputBase64 := base64.StdEncoding.EncodeToString(marshalPluginInput(pi))

	out, err := exec.Command(pluginName, inputBase64).CombinedOutput()
	if err != nil {
		output.print("result", "error")
		output.print("description", "error executing the plugin")
		return
	}

	var pluginOutput interface{}
	json.Unmarshal(out, &pluginOutput)

	output.O = json.RawMessage(out)
	if !*machineReadable {
		b, _ := json.MarshalIndent(&pluginOutput, "", "    ")
		fmt.Printf("%15s:\n%s\n", "output", string(b))
	}

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
	o.M[identifier] = output

	if !*machineReadable {
		fmt.Printf("%15s: %s\n", identifier, output)
	}
}

func printOutput(o Output) {
	if *machineReadable {
		bs, err := json.MarshalIndent(&o, "", "    ")
		if err != nil {
			var eo ErrorOutput
			eo.Meta = o.M
			eo.InvalidOutput = string(o.O)
			eo.ErrorString = fmt.Sprintf("%s", err)

			bss, errr := json.MarshalIndent(&eo, "", "    ")
			if errr == nil {
				fmt.Printf("%s", string(bss))
			} else {
				fmt.Println("watts-plugin-tester: ERROR")
			}

		} else {
			fmt.Printf("%s", string(bs))
		}
	}
	return
}

func main() {
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginTest.FullCommand():
		o := doPluginTest(*pluginTestName)
		printOutput(o)
	case printDefault.FullCommand():
		dpi := marshalPluginInput(defaultPluginInput)
		fmt.Printf("%s", string(dpi))
	case printSpecific.FullCommand():
		spi := marshalPluginInput(specificJson(defaultPluginInput))
		fmt.Printf("%s", string(spi))
	case printVersion.FullCommand():
		fmt.Printf("%s\n", version)
	}
}
