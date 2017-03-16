#!/usr/bin/env python2
# -*- coding: utf-8 -*-

import json
import base64
import sys
import os
import traceback
import string
import random
from pwd import getpwnam
from subprocess import check_output as qx

from requests.auth import HTTPBasicAuth
import requests

ALLOWED_LOA=["https://aai.egi.eu/LoA#Substantial"]

STATE_PREFIX="TTS_"

WORK_DIR="/home/tts/.config/tts/ssh/"

def list_params():
    RequestParams = [[{'key':'pub_key', 'name':'public key',
                       'description':'the public key to upload to the service',
                       'type':'textarea', 'mandatory':True}], []]
    ConfParams = [{'name':'state_prefix', 'type':'string', 'default':'TTS_'},
                  {'name':'host_list', 'type':'string', 'default':'_not_configured'},
                  {'name':'idh_url', 'type':'string', 'default':'_not_configured'},
                  {'name':'idh_service_url', 'type':'string', 'default':'_not_configured'},
                  {'name':'auth_username', 'type':'string', 'default':'_not_configured'},
                  {'name':'auth_password', 'type':'string', 'default':'_not_configured'},
                  {'name':'check_loa', 'type':'boolean', 'default':'true'}
                 ]
    Version = "0.1.0"
    return json.dumps({'result':'ok', 'conf_params': ConfParams, 'request_params': RequestParams, 'version':Version})


def create_ssh(UserId, Params, Hosts):
    if Params.has_key('public key'):
        return insert_ssh_key(UserId, Params['public key'], Hosts)
    else:
        return create_ssh_for(UserId, Hosts)


def create_register_ldap(UserId, IdhUrl, IdhServiceUrl, Auth):
    headers = {'Content-Type': 'application/json'}

    creation_data = {"persistentId": UserId, "genericStore": {}}
    creation = requests.post(IdhUrl + '/users', auth=Auth, headers=headers, data=json.dumps(creation_data))
    creation_status = int(creation.status_code)
    creation_json = creation.json()[0]

    try:
        if creation_status == 404:
            if creation_json['message'] == 'persistent id ' + UserId + ' already assigned':
                return {'result': 'ok'}
            else:
                LogMsg = 'Failed to create user: ' + str(creation_status) + ' ' + str(creation.content)
                return {'result': 'error', 'log_msg': LogMsg}

        elif creation_status == 201:
            try:
                user_uri = creation.json()['_links']['self']['href']

                registration_data = {'user': user_uri, 'service': IdhServiceUrl}
                registration = requests.post(IdhUrl + '/registries',
                        auth=Auth, headers=headers,
                        data=json.dumps(registration_data))
                registration_status = registration.status_code
                registration_json = registration.json()[0]

                if registration_status == 200:
                    return {'result': 'ok'}
                else:
                    LogMsg = 'Failed to register user ' + str(registration_status) + '; ' + str(registration_json)
                    return {'result': 'error', 'log_msg': LogMsg}

            except Exception, E:
                LogMsg = "registration failed with exception: " + str(E) + '; ' + str(registration.content)
                return {'result': 'error', 'log_msg': LogMsg}

        else:
            LogMsg = 'Failed to create user: ' + str(creation_status) + '; ' + str(creation.content)
            return {'result': 'error', 'log_msg': LogMsg}

    except Exception, E:
        LogMsg = 'creation failed with exception: ' + str(creation.content) + str(creation.status_code) + '; ' + str(E)
        return {'result': 'error', 'log_msg': LogMsg}


def revoke_ssh(UserId, State, Hosts):
    return delete_ssh_for(UserId, State, Hosts)


def create_ssh_for(UserId, Hosts):
    Password = id_generator()
    State = "%s%s"%(STATE_PREFIX,id_generator(32))
    # maybe change this to random/temp file
    WorkDir = os.path.join(WORK_DIR, UserId)
    OutputFile = os.path.join(WorkDir,"tts_ssh_key")
    OutputPubFile = os.path.join(WorkDir,"tts_ssh_key.pub")
    EnsureWorkDir = "mkdir -p %s > /dev/null"%(WorkDir)
    RmDir = "rm -rf %s > /dev/null"%WorkDir
    # DelKey = "srm -f %s %s.pub > /dev/null"%(OutputFile,OutputFile)
    # DelKey = "shred -f %s %s.pub > /dev/null"%(OutputFile,OutputFile)
    os.system(RmDir)
    os.system(EnsureWorkDir)
    Cmd = "ssh-keygen -N %s -C %s -f %s > /dev/null"%(Password,State,OutputFile)
    Res = os.system(Cmd)
    if Res != 0:
        LogMsg = "the key generation '%s' failed with %d"%(Cmd, Res)
        UserMsg = "sorry, the key generation failed"
        return json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

    PubKey = validate_and_update_key(get_file_content(OutputPubFile), State)
    PrivKey = get_file_content(OutputFile)
    if PubKey == None:
        LogMsg = "the public key generated '%s' is not valid"%InKey
        UserMsg = "sorry, the public key generation failed"
        return json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})


    os.system(RmDir)

    PubKeyObj = {'name':'Public Key', 'type':'textfile', 'value':PubKey, 'rows':4}
    PrivKeyObj = {'name':'Private Key', 'type':'textfile', 'value':PrivKey, 'rows':30, 'cols':64}
    PasswdObj = {'name':'Passphrase (for Private Key)', 'type':'text', 'value':Password}
    Credential = [PrivKeyObj, PasswdObj, PubKeyObj]

    HostResult = deploy_key(UserId, PubKey, State, Hosts)
    if HostResult['result'] == 'ok':
        HostCredential = HostResult['output']
        Credential.extend(HostCredential)
        return json.dumps({'result':'ok', 'credential':Credential, 'state':State})
    else:
        Log = HostResult['log']
        UserMsg = "the deployment failed, the error has been logged, please contact the administrator"
        LogMsg = "key deployment did fail on at least one host: '%s'"%Log
        return json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})
    return json.dumps({'result':'ok', 'credential':Credential, 'state':State})


def insert_ssh_key(UserId, InKey, Hosts):
    State = "%s%s"%(STATE_PREFIX,id_generator(32))
    PubKey = validate_and_update_key(InKey, State)
    if PubKey == None:
        LogMsg = "the key given by the user '%s' is not valid"%InKey
        UserMsg = "sorry, the public key was not valid"
        return json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

    Result = deploy_key(UserId, PubKey, State, Hosts)
    if Result['result'] == 'ok':
        Credential = Result['output']
        return json.dumps({'result':'ok', 'credential':Credential, 'state':State})
    else:
        Log = Result['log']
        UserMsg = "the deployment failed, the error has been logged, please contact the administrator"
        LogMsg = "key deployment did fail on at least one host: '%s'"%Log
        return json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})


def validate_and_update_key(Key, State):
    if len(Key) < 3:
        return None
    KeyParts = Key.split(" ", 2)
    if len(KeyParts) != 3:
        return None
    KeyType = KeyParts[0]
    PubKey = KeyParts[1]
    if not KeyType.startswith("ssh-"):
        return None
    if len(PubKey) < 4:
        return None
    return "%s %s %s"%(KeyType, PubKey, State)


def deploy_key(UserId, Key, State, Hosts):
    Json = json.dumps({'action':'request', 'watts_userid':UserId, 'cred_state':'undefined', 'params':{'state':State, 'pub_key':Key}})
    Parameter = base64.urlsafe_b64encode(Json)
    Result = execute_on_hosts(Parameter, Hosts)
    Output = []
    Log = ""
    for Json in Result:
        if Json.has_key('result') and Json['result'] == 'ok' :
            Host = Json['host']
            Credential = Json['credential']
            UserName = None
            for Cred in Credential :
                if Cred['name'] == 'Username':
                    Username = Cred['value']
            Output.append( {'name':"user @ %s"%Host, 'type':'text', 'value':Username })
        else:
            Log = "%s%s: %s; "%(Log, Json['host'], Json['log_msg'])

    if len(Output) == len(Result):
        return { 'result':'ok', 'output':Output }
    else:
        return { 'result':'error', 'log':Log }

def delete_ssh_for(UserId, State, Hosts):
    Json = json.dumps({'action':'revoke', 'watts_userid':UserId, 'cred_state':State, 'params':''})
    Parameter = base64.urlsafe_b64encode(Json)
    Result = execute_on_hosts(Parameter, Hosts)
    OkCount = 0
    Log = ""
    for Json in Result :
        if Json.has_key('result') and Json['result'] == 'ok' :
            OkCount = OkCount + 1
        else:
            Log = "%s%s: %s; "%(Log, Json['host'], Json['log_msg'])
    if OkCount == len(Result) :
        return json.dumps({'result':'ok'})
    else:
        UserMsg = "the revocation failed and has been logged, please contact the administrator"
        LogMsg = "key revocation did fail on at least one host: '%s'"%Log
        return json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

def execute_on_hosts(Parameter, Hosts):

    # loop through all server and collect the output
    Cmd = "sudo /home/tts/.config/tts/ssh_idh_vm.py %s"%Parameter
    Result = []
    for Host in Hosts:
        UserHost = "tts@%s"%Host
        Output = qx(["ssh","-i","/home/tts/.ssh/id_rsa", UserHost, Cmd])
        Json = json.loads(Output)
        Json['host'] = Host
        Result.append(Json)
    return Result





def get_file_content(File):
    fo = open(File)
    Content = fo.read()
    fo.close()
    return Content


def id_generator(size=16, chars=string.ascii_uppercase + string.digits+string.ascii_lowercase):
    return ''.join(random.choice(chars) for _ in range(size))

def is_allowed_loa(Loa):
    if Loa in ALLOWED_LOA:
        return True
    return False

def ensure_group_list(MaybeGroups):
    if type(MaybeGroups) is str:
        Groups=parse_group_string(MaybeGroups)
        return Groups
    if type(MaybeGroups) is unicode:
        Groups=parse_group_string(MaybeGroups)
        return Groups
    if type(MaybeGroups) is list:
        return MaybeGroups
    else:
        return []

def parse_group_string(GroupString):
    RawGroups = GroupString.split(',')
    Groups = []
    for Group in RawGroups:
        Groups.append(Group.strip())
    return Groups

def get_loa(Oidc):
    Loa = ""
    if 'acr' in Oidc:
        Loa = Oidc['acr']
    return Loa

def get_groups(Oidc):
    Groups = []
    if 'groups' in Oidc:
        Groups = ensure_group_list(Oidc['groups'])
    return Groups


def main():
    UserMsg = "Internal error, please contact the administrator"
    try:
        Cmd = None
        if len(sys.argv) == 2:
            Json = str(sys.argv[1])+ '=' * (4 - len(sys.argv[1]) % 4)
            JObject = json.loads(str(base64.urlsafe_b64decode(Json)))

            #general information
            Action = JObject['action']
            if Action == "parameter":
                print list_params()

            else:
                State = JObject['cred_state']
                Params = JObject['params']
                ConfParams = JObject['conf_params']
                UserId = JObject['watts_userid']

                STATE_PREFIX = ConfParams['state_prefix']
                CheckLoa = ConfParams['check_loa']

                Auth_Password = ConfParams['auth_password']
                Auth_Username = ConfParams['auth_username']

                IdhUrl = ConfParams['idh_url']
                IdhServiceUrl = ConfParams['idh_service_url']

                UserInfo = JObject['user_info']
                Subject = UserInfo['sub']
                Issuer = UserInfo['iss']
                Groups = get_groups(UserInfo)
                Loa = get_loa(UserInfo)

                Host = ConfParams['host_list']
                Hosts = Host.split()

                if Host == '_not_configured':
                    LogMsg = "the plugin has no hosts configured, use the 'host_list' parameter"
                    print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

                elif Auth_Username == '_not_configured' or Auth_Password == '_not_configured':
                    LogMsg = "the plugin has no authentication configured, use the 'auth_username' and 'auth_password' parameters"
                    print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

                elif IdhUrl == '_not_configured':
                    LogMsg = "the plugin has no idh url configured, use the 'idh_url' parameter"
                    print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

                elif IdhServiceUrl == '_not_configured':
                    LogMsg = "the plugin has no idh service url configured, use the 'idh_service_url' parameter"
                    print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

                elif Action == "request":
                    if is_allowed_loa(Loa) or not CheckLoa:
                        Auth = HTTPBasicAuth(Auth_Username, Auth_Password)
                        Ldap = create_register_ldap(UserId, IdhUrl, IdhServiceUrl, Auth)

                        if Ldap['result'] == 'ok':
                            print create_ssh(UserId, Params, Hosts)
                        else:
                            LogMsg = 'error: ' + json.dumps(Ldap)
                            print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

                    else:
                        LogMsg = "user %s - %s with loa %s is not allowed"%(Issuer, Subject, Loa)
                        UserMsg = "sorry, your level of assurance (loa) is too low"
                        print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})

                elif Action == "revoke":
                    print revoke_ssh(UserId, State, Hosts)
                else:
                    LogMsg = "the plugin was run with an unknown action '%s'"%Action
                    print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})
        else:
            LogMsg = "the plugin was run without an action"
            print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})
    except Exception, E:
        TraceBack = traceback.format_exc(),
        LogMsg = "the plugin failed with %s - %s"%(str(E), TraceBack)
        print json.dumps({'result':'error', 'user_msg':UserMsg, 'log_msg':LogMsg})
        pass

if __name__ == "__main__":
    main()
