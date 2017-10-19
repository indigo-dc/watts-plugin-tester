package schemes

import (
	v "github.com/gima/govalid/v1"
)

var (
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

	// PluginInputScheme Scheme to check the input of a plugin against
	PluginInputScheme = v.Or(
		v.Object(
			v.ObjKV("action", v.String(v.StrIs("parameter"))),
			v.ObjKV("watts_version", v.String()),
		),
		v.Object(
			v.ObjKV("action", v.String()),
			v.ObjKV("watts_version", v.String()),
			v.ObjKV("watts_userid", v.String()),
			v.ObjKV("cred_state", v.String()),
			v.ObjKV("access_token", schemeAccessToken),
			v.ObjKV("additional_logins", schemeAdditionalLogins),
			v.ObjKV("conf_params", schemeParams),
			v.ObjKV("params", schemeParams),
			v.ObjKV("user_info", schemeUserInfo),
		),
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

	// WattsSchemes Scheme to validate specific input from watts against
	WattsSchemes = map[string](map[string]v.Validator){
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
