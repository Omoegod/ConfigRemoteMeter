package app

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/tarm/serial"
)

var stopChannel chan bool

type entryRow struct {
	devNum *widget.Entry
	rfNum  *widget.Entry
	chNum  *widget.Entry
	check  *widget.Check
}

type ConfigApp struct {
	QuantityRow int `json:"quantityrow"`
}

type Application struct {
	App              fyne.App
	Window           fyne.Window
	Config           *ConfigApp
	PortMutex        sync.Mutex
	IsOpenPort       bool
	CurrentPort      *serial.Port
	EntryList        *fyne.Container
	EntryRows        []*entryRow
	AddRowButtonMust *widget.Button
	HeaderRow        *fyne.Container
	ButtonRow        *fyne.Container
	WrappedTopBar    *fyne.Container
}
