package workspaceaccess

import "errors"

var ErrDenied = errors.New("workspace access denied")
var ErrDefaultProtected = errors.New("Default Workspace cannot be renamed, archived or have its membership changed")
var ErrInheritedRole = errors.New("inherited workspace membership cannot be changed or removed")
var ErrInvalidRole = errors.New("invalid workspace role")
