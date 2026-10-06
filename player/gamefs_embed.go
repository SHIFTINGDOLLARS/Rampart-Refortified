//go:build !byof

package main

import (
	"embed"
	"io/fs"
)

//go:embed game/*
var embeddedGame embed.FS

const byofBuild = false

func loadGameFS() (fs.FS, string, error) { return embeddedGame, "", nil }
