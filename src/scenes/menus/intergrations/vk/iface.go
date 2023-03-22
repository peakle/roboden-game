package vk

import (
	"crypto/md5"
	"strconv"
	"time"
)

func InviteFriends() string {
	h := md5.New()
	h.Write([]byte(strconv.FormatInt(time.Now().Unix(), 10)))
	return inviteFriendsPopup(string(h.Sum(nil)))
}
