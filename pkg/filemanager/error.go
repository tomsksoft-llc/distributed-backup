package filemanager

import (
	"github.com/pkg/errors"
)

var errIsDirectory = errors.New("is a directory")
var errNotDirectory = errors.New("not a directory")
