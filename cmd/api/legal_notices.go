package main

import (
	_ "embed"
	"runtime"
)

var (
	//go:embed legal/LICENSE.txt
	embeddedProjectLicense string
	//go:embed legal/NOTICE.txt
	embeddedProjectNotice string
	//go:embed legal/SYSTRAY_LICENSE.txt
	embeddedSystrayLicense string
)

func retainEmbeddedLegalNotices() {
	runtime.KeepAlive(embeddedProjectLicense)
	runtime.KeepAlive(embeddedProjectNotice)
	runtime.KeepAlive(embeddedSystrayLicense)
}
