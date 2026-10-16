package demo

// User is a synthetic PR-review demonstration value.
type User struct{ Name string }

func UserName(u *User) string {
	if u == nil {
		return ""
	}
	return u.Name
}
