WaTTS Plugin Tester Jenkins Infrastructure
==========================================

Test WaTTS plugins (e.g. for Jenkins)

Environment variables
---------------------
- `TARGET_PLUGIN_REPO`

Workflow
--------
- Clone `$TARGET_PLUGIN_REPO`
- Read the file `$TARGET_PLUGIN_REPO/test/config.json`. Default:
```js
{
    "init_cmd": null,
    "exec_file": <required>,
    "test_dir": "test"
}
```
- Search for input files of the form `<test_dir>/{parameter,request,revoke}_*_{pass,fail}.json`
- Run `<init_cmd>` if not `null`
- Run `<exec_file>` for each input file
- Report results
