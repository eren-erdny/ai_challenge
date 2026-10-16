# Synthetic PR review demonstration

The file prreview/testdata/demo/user.go is a demonstration fixture, not application code.
UserName must return an empty string when User is nil.
HTTP handlers may call UserName with optional users; a missing authenticated user is represented by nil.
Removing the nil guard causes UserName(nil) to panic.
