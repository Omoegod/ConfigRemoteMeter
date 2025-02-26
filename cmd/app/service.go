package app

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/tarm/serial"
)

func removeEmptyLines(lines []string) []string {
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

func LoadConfig(filename string) (*ConfigApp, error) {
	config := &ConfigApp{
		QuantityRow: 10,
	}

	file, err := os.Open(filename)
	if err != nil {
		SaveConfig(filename, config)
		return config, nil
	}

	defer file.Close()

	err = json.NewDecoder(file).Decode(config)

	return config, nil
}

func SaveConfig(filename string, config *ConfigApp) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}

	defer file.Close()

	return json.NewEncoder(file).Encode(config)
}

func (a *Application) update() {
	a.EntryRows = nil
	a.EntryList.Objects = nil
	for i := 0; i < a.Config.QuantityRow; i++ {
		a.AddEntryRow()
	}
	a.EntryList.Refresh()

	addRowButton := widget.NewButton("Добавить строку", func() {
		a.AddEntryRow()
	})

	addRowButtonMust := widget.NewButton(fmt.Sprintf("Добавить %d", a.Config.QuantityRow), func() {
		for i := 0; i < a.Config.QuantityRow; i++ {
			a.AddEntryRow()
		}
	})

	a.AddRowButtonMust = addRowButtonMust

	loadButton := widget.NewButton("Загрузить из файла", func() {
		a.loadFromFile()
	})

	a.ButtonRow = container.NewHBox(addRowButton, a.AddRowButtonMust, loadButton)

	scrollableEntryList := container.NewVScroll(a.EntryList)
	scrollableEntryList.SetMinSize(fyne.NewSize(420, 650))

	mainContainer := container.NewVBox(a.ButtonRow, a.HeaderRow, scrollableEntryList)

	divider := canvas.NewRectangle(color.Gray{Y: 228})
	divider.SetMinSize(fyne.NewSize(0, 2))

	a.Window.SetContent(container.NewVBox(a.WrappedTopBar, divider, mainContainer))

}

func getAvailablePorts() []string {
	var ports []string
	if runtime.GOOS == "windows" {
		cmd := exec.Command("powershell", "-Command", "Get-WMIObject Win32_SerialPort | Select-Object -Expand DeviceID")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := cmd.Output()
		if err == nil {
			ports = strings.Split(strings.TrimSpace(string(out)), "\r\n")
		}
	} else {
		out, err := exec.Command("sh", "-c", "ls /dev/ttyUSB* /dev/ttyS* 2>/dev/null").Output()
		if err == nil {
			ports = strings.Split(strings.TrimSpace(string(out)), "\n")
		}
	}

	if len(ports) == 0 {
		ports = []string{"Нет доступных портов"}
	}
	return ports
}

func (a *Application) readDataFromPort(port *serial.Port) {
	buf := make([]byte, 512)
	dataReceived := make([]byte, 0)
	for {
		n, err := port.Read(buf)
		if err != nil {
			log.Println("Ошибка чтения:", err)
			_ = port.Close()
			log.Println("Порт закрыт после чтения данных.")
			a.IsOpenPort = false
			return
		}
		dataReceived = append(dataReceived, buf[:n]...)
		fmt.Printf("Получено: 0x%X\n", buf[:n])

		if len(dataReceived) >= 7 && dataReceived[0] == 0x1C && dataReceived[1] == 0x1C && dataReceived[2] == 0x0B &&
			dataReceived[3] == 0x61 && dataReceived[4] == 0xA4 && dataReceived[5] == 0x44 && dataReceived[6] == 0x54 {

			fmt.Printf("Получено: 0x%X\n", dataReceived)

			for _, row := range a.EntryRows {
				if !row.check.Checked {

					data := []byte{
						0x81,       // send data
						0x01,       // number packet
						0x00,       // flag
						0x00, 0x00, // address send
						0x20, 0x4E, // address receiver
					}

					payload := []byte{
						0x1C, // address 0 or 255
						0x1C, // function (share)
						0x0B, // len packet
						0x61, // number packet
					}

					summ := byte(0x1C + 0x1C + 0x0B + 0x61)
					payload = append(payload, summ)

					data = append(data, payload...)

					data = append(data, []byte(row.devNum.Text)...)
					data = append(data, []byte(row.rfNum.Text)...)
					data = append(data, []byte(row.chNum.Text)...)

					crc := CalculateCRC16(data)
					data = append(data, byte(crc), byte(crc>>8))

					xor := CalculateXOR(data)
					data = append(data, xor)

					_, err = port.Write(data)
					if err != nil {
						log.Println("Ошибка отправки данных:", err)
					} else {
						log.Println("Данные успешно отправлены:", data)
						dataReceived = nil
					}

					row.check.SetChecked(true)
					v := fyne.CurrentApp().Settings().ThemeVariant()
					row.check.Theme().Color("foregroundOnSuccess", v)
					row.check.Refresh()

					break
				}
			}
		}
	}
}

func (a *Application) OpenPort(selectedPort string, OpenPortItem *widget.Button) {
	a.PortMutex.Lock()
	defer a.PortMutex.Unlock()

	if a.IsOpenPort && a.CurrentPort != nil {
		log.Println("Закрытие предыдущего порта...")
		OpenPortItem.SetText("Открыть порт")
		a.ClosePort()
	}

	if selectedPort == "Нет доступных портов" {
		log.Println("Нет доступных портов")
		OpenPortItem.SetText("Открыть порт")
		return
	}

	config := &serial.Config{
		Name:        selectedPort,
		Baud:        9600,
		ReadTimeout: time.Second * 2,
	}

	port, err := serial.OpenPort(config)
	if err != nil {
		log.Println("Ошибка открытия порта:", err)
		OpenPortItem.SetText("Открыть порт")
		return
	}

	a.CurrentPort = port
	a.IsOpenPort = true
	OpenPortItem.SetText("Закрыть порт")
	fmt.Println("Порт открыт:", selectedPort)

	stopChannel = make(chan bool)

	go a.readDataFromPort(port)
}

func (a *Application) ClosePort() {
	if a.CurrentPort != nil {
		if stopChannel != nil {
			close(stopChannel)
		}

		err := a.CurrentPort.Close()
		if err != nil {
			log.Println("Ошибка при закрытии порта:", err)
		} else {
			log.Println("Порт закрыт.")
		}

		a.CurrentPort = nil
		a.IsOpenPort = false
	}
}

func (a *Application) AddEntryRow() {
	devNum := widget.NewEntry()
	rfNum := widget.NewEntry()
	chNum := widget.NewEntry()
	check := widget.NewCheck("", func(value bool) {
		log.Println("Check set to", value)
	})

	devNum.OnChanged = func(text string) {
		cleanText := strings.TrimSpace(text)
		if len(cleanText) > 8 {
			cleanText = cleanText[:8]
		}
		devNum.SetText(cleanText)
		if len(cleanText) >= 8 {
			if cleanText[len(cleanText)-5:] == "00000" {
				rfNum.SetText("10000")
			} else {
				rfNum.SetText(cleanText[len(cleanText)-5:])
			}
		} else {
			rfNum.SetText("")
		}

		if len(cleanText) >= 8 {
			chNum.SetText("00")
		} else {
			chNum.SetText("")
		}
	}

	row := &entryRow{devNum, rfNum, chNum, check}
	a.EntryRows = append(a.EntryRows, row)

	devNumContainer := container.NewGridWrap(fyne.NewSize(140, 40), devNum)
	rfNumContainer := container.NewGridWrap(fyne.NewSize(125, 40), rfNum)
	chNumContainer := container.NewGridWrap(fyne.NewSize(80, 40), chNum)
	checkContainer := container.NewGridWrap(fyne.NewSize(40, 40), check)

	entryRow := container.NewHBox(devNumContainer, rfNumContainer, chNumContainer, checkContainer)

	a.EntryList.Add(entryRow)
	a.EntryList.Refresh()
}

func (a *Application) ShowSettingsWindow(w fyne.Window) {
	settingsWindow := fyne.CurrentApp().NewWindow("Настройки")
	settingsWindow.Resize(fyne.NewSize(350, 140))

	entry := widget.NewEntry()
	entry.SetText(fmt.Sprintf("%d", a.Config.QuantityRow))

	entry.OnChanged = func(text string) {
		if text == "" {
			return
		}
		num, err := strconv.Atoi(text)
		if err != nil || num < 1 || num > 2000 {
			entry.SetText("1")
		} else {
			a.Config.QuantityRow = num
		}
	}

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Кол-во строк по-умолчанию", Widget: entry}},
		OnSubmit: func() {

			SaveConfig("config.json", a.Config)
			log.Println("Установлено количество строк:", a.Config.QuantityRow)

			a.update()

			settingsWindow.Close()
		},
		SubmitText: "Сохранить",
		OnCancel: func() {
			settingsWindow.Close()
		},
		CancelText: "Закрыть",
	}

	content := container.NewVBox(form)
	settingsWindow.SetContent(content)

	settingsWindow.Show()
}

func (a *Application) loadFromFile() {
	dialog.ShowFileOpen(func(file fyne.URIReadCloser, err error) {
		if err != nil || file == nil {
			log.Println("Ошибка при открытии файла:", err)
			return
		}
		defer file.Close()

		fileContent, err := io.ReadAll(file)
		if err != nil {
			log.Println("Ошибка при чтении файла:", err)
			return
		}

		lines := strings.Split(string(fileContent), "\n")
		lines = removeEmptyLines(lines)

		a.EntryRows = nil
		a.EntryList.Objects = nil

		for i, line := range lines {
			if i < len(a.EntryRows) {
				a.EntryRows[i].devNum.SetText(strings.TrimSpace(line))
			} else {
				a.AddEntryRow()
				a.EntryRows[i].devNum.SetText(strings.TrimSpace(line))
			}
		}

	}, a.Window)
}

func (a *Application) Init() {
	a.App = app.New()
	a.Window = a.App.NewWindow("Привязка пульта к счетчику")
	a.Window.Resize(fyne.NewSize(430, 800))
	a.Window.SetFixedSize(true)

	config, _ := LoadConfig("config.json")
	a.Config = config
}

func (a *Application) Run() {
	ports := getAvailablePorts()

	selectedPort := ports[0]

	portDropdown := widget.NewSelect(ports, func(value string) {
		selectedPort = value
	})
	portDropdown.PlaceHolder = "Выберите порт"

	updatePorts := func(dropdown *widget.Select) {
		newPorts := getAvailablePorts()
		dropdown.Options = newPorts
		dropdown.Refresh()
	}

	go func() {
		for {
			time.Sleep(5 * time.Second)
			updatePorts(portDropdown)
		}
	}()

	var openPortItem *widget.Button

	openPortItem = widget.NewButton("Открыть порт", func() {
		go a.OpenPort(selectedPort, openPortItem)
	})

	settingsButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		a.ShowSettingsWindow(a.Window)
	})

	loadButton := widget.NewButton("Загрузить из файла", func() {
		a.loadFromFile()
	})

	topBar := container.NewHBox(
		portDropdown,
		openPortItem,
		settingsButton,
	)

	a.WrappedTopBar = container.NewGridWrap(fyne.NewSize(400, 40), topBar)

	a.EntryList = container.NewVBox()

	a.HeaderRow = container.NewHBox(
		widget.NewLabelWithStyle("Номер счетчика", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Сетевой адрес", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Подсеть", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	)

	addRowButton := widget.NewButton("Добавить строку", func() {
		a.AddEntryRow()
	})
	a.AddRowButtonMust = widget.NewButton(fmt.Sprintf("Добавить %d", a.Config.QuantityRow), func() {
		for i := 0; i < a.Config.QuantityRow; i++ {
			a.AddEntryRow()
		}
	})

	buttonRow := container.NewHBox(addRowButton, a.AddRowButtonMust, loadButton)

	scrollableEntryList := container.NewVScroll(a.EntryList)
	scrollableEntryList.SetMinSize(fyne.NewSize(420, 650))

	mainContainer := container.NewVBox(buttonRow, a.HeaderRow, scrollableEntryList)

	divider := canvas.NewRectangle(color.Gray{Y: 228})
	divider.SetMinSize(fyne.NewSize(0, 2))

	a.Window.SetContent(container.NewVBox(a.WrappedTopBar, divider, mainContainer))

	a.update()

	a.Window.ShowAndRun()
}
