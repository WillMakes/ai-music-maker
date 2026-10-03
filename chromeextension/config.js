// Written by AIMusicMaker when the extension is exported (see extensionbridge.go).
// It carries the address of the app's local bridge and the token that authorises
// this copy of the extension to reach it.
//
// The placeholders below are replaced on export; the committed file is a
// template, so the repository never holds a real token.
self.__AIMUSICMAKER = {port: __SM_BRIDGE_PORT__, token: '__SM_BRIDGE_TOKEN__'}
