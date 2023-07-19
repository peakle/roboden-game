//go:build !steam

package steamsdk

import (
	"errors"
)

func UnlockAchievement(name string) bool {
	return false
}

func IsAchievementUnlocked(name string) (bool, error) {
	return false, errors.New("steamsdk is not available")
}
