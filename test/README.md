WaTTS Plugin Tester Jenkins Infrastructure
==========================================

Test WaTTS plugins (e.g. for Jenkins)

Environment variables
---------------------
- `TARGET_PLUGIN_REPO`

Workflow
--------
- Clone `$TARGET_PLUGIN_REPO`
- Build the watts-plugin-tester from this git repo
- Start the watts-plugin-tester with the config file `$TARGET_PLUGIN_REPO/test/config.json`
- Delete all created files afterwards
