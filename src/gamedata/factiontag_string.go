// Code generated by "stringer -type=FactionTag"; DO NOT EDIT.

package gamedata

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[NeutralFactionTag-0]
	_ = x[YellowFactionTag-1]
	_ = x[RedFactionTag-2]
	_ = x[GreenFactionTag-3]
	_ = x[BlueFactionTag-4]
}

const _FactionTag_name = "NeutralFactionTagYellowFactionTagRedFactionTagGreenFactionTagBlueFactionTag"

var _FactionTag_index = [...]uint8{0, 17, 33, 46, 61, 75}

func (i FactionTag) String() string {
	if i < 0 || i >= FactionTag(len(_FactionTag_index)-1) {
		return "FactionTag(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _FactionTag_name[_FactionTag_index[i]:_FactionTag_index[i+1]]
}
