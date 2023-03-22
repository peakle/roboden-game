//go:build js && wasm

package vk

import "syscall/js"

func inviteFriendsPopup(key string) string {
	r := js.Global().Call("invitePopup", key)
	return r.String()
}
