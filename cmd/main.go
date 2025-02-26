package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/tarm/serial"
)

var entryRows []*entryRow

type entryRow struct {
	devNum *widget.Entry
	rfNum  *widget.Entry
	chNum  *widget.Entry
	check  *widget.Check
}

type ConfigApp struct {
	quantityrow int `json:"quantityrow"`
}

var currentPort *serial.Port
var portMutex sync.Mutex
var isPortOpen bool = false
var stopChannel chan bool

func LoadConfig(filename string) (*ConfigApp, error) {
	config := &ConfigApp{
		quantityrow: 10,
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

func getAvailablePorts() []string {
	var ports []string
	if runtime.GOOS == "windows" {
		out, err := exec.Command("powershell", "-Command", "Get-WMIObject Win32_SerialPort | Select-Object -Expand DeviceID").Output()
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

func readDataFromPort(port *serial.Port) {
	buf := make([]byte, 512)
	dataReceived := make([]byte, 0)
	for {
		n, err := port.Read(buf)
		if err != nil {
			log.Println("Ошибка чтения:", err)
			_ = port.Close()
			log.Println("Порт закрыт после чтения данных.")
			isPortOpen = false
			return
		}
		dataReceived = append(dataReceived, buf[:n]...)
		fmt.Printf("Получено: 0x%X\n", buf[:n])

		if len(dataReceived) >= 7 && dataReceived[0] == 0x1C && dataReceived[1] == 0x1C && dataReceived[2] == 0x0B &&
			dataReceived[3] == 0x61 && dataReceived[4] == 0xA4 && dataReceived[5] == 0x44 && dataReceived[6] == 0x54 {

			fmt.Printf("Получено: 0x%X\n", dataReceived)

			for _, row := range entryRows {
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

					crc := calculateCRC16(data)
					data = append(data, byte(crc), byte(crc>>8))

					xor := calculateXOR(data)
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

var tblCRChi = [256]byte{0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81,
	0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0,
	0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01,
	0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41,
	0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81,
	0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0,
	0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01,
	0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40,
	0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81,
	0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0,
	0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01,
	0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41,
	0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81,
	0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0,
	0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01,
	0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41,
	0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81,
	0x40}

var tblCRClo = [256]byte{0x00, 0xC0, 0xC1, 0x01, 0xC3, 0x03, 0x02, 0xC2, 0xC6, 0x06, 0x07, 0xC7, 0x05, 0xC5, 0xC4,
	0x04, 0xCC, 0x0C, 0x0D, 0xCD, 0x0F, 0xCF, 0xCE, 0x0E, 0x0A, 0xCA, 0xCB, 0x0B, 0xC9, 0x09,
	0x08, 0xC8, 0xD8, 0x18, 0x19, 0xD9, 0x1B, 0xDB, 0xDA, 0x1A, 0x1E, 0xDE, 0xDF, 0x1F, 0xDD,
	0x1D, 0x1C, 0xDC, 0x14, 0xD4, 0xD5, 0x15, 0xD7, 0x17, 0x16, 0xD6, 0xD2, 0x12, 0x13, 0xD3,
	0x11, 0xD1, 0xD0, 0x10, 0xF0, 0x30, 0x31, 0xF1, 0x33, 0xF3, 0xF2, 0x32, 0x36, 0xF6, 0xF7,
	0x37, 0xF5, 0x35, 0x34, 0xF4, 0x3C, 0xFC, 0xFD, 0x3D, 0xFF, 0x3F, 0x3E, 0xFE, 0xFA, 0x3A,
	0x3B, 0xFB, 0x39, 0xF9, 0xF8, 0x38, 0x28, 0xE8, 0xE9, 0x29, 0xEB, 0x2B, 0x2A, 0xEA, 0xEE,
	0x2E, 0x2F, 0xEF, 0x2D, 0xED, 0xEC, 0x2C, 0xE4, 0x24, 0x25, 0xE5, 0x27, 0xE7, 0xE6, 0x26,
	0x22, 0xE2, 0xE3, 0x23, 0xE1, 0x21, 0x20, 0xE0, 0xA0, 0x60, 0x61, 0xA1, 0x63, 0xA3, 0xA2,
	0x62, 0x66, 0xA6, 0xA7, 0x67, 0xA5, 0x65, 0x64, 0xA4, 0x6C, 0xAC, 0xAD, 0x6D, 0xAF, 0x6F,
	0x6E, 0xAE, 0xAA, 0x6A, 0x6B, 0xAB, 0x69, 0xA9, 0xA8, 0x68, 0x78, 0xB8, 0xB9, 0x79, 0xBB,
	0x7B, 0x7A, 0xBA, 0xBE, 0x7E, 0x7F, 0xBF, 0x7D, 0xBD, 0xBC, 0x7C, 0xB4, 0x74, 0x75, 0xB5,
	0x77, 0xB7, 0xB6, 0x76, 0x72, 0xB2, 0xB3, 0x73, 0xB1, 0x71, 0x70, 0xB0, 0x50, 0x90, 0x91,
	0x51, 0x93, 0x53, 0x52, 0x92, 0x96, 0x56, 0x57, 0x97, 0x55, 0x95, 0x94, 0x54, 0x9C, 0x5C,
	0x5D, 0x9D, 0x5F, 0x9F, 0x9E, 0x5E, 0x5A, 0x9A, 0x9B, 0x5B, 0x99, 0x59, 0x58, 0x98, 0x88,
	0x48, 0x49, 0x89, 0x4B, 0x8B, 0x8A, 0x4A, 0x4E, 0x8E, 0x8F, 0x4F, 0x8D, 0x4D, 0x4C, 0x8C,
	0x44, 0x84, 0x85, 0x45, 0x87, 0x47, 0x46, 0x86, 0x82, 0x42, 0x43, 0x83, 0x41, 0x81, 0x80,
	0x40}

func calculateCRC16(msg []byte) uint16 {
	var CRChi byte = 0xFF
	var CRClo byte = 0xFF
	var idx byte

	for _, b := range msg {
		idx = CRChi ^ b
		CRChi = CRClo ^ tblCRChi[idx]
		CRClo = tblCRClo[idx]
	}

	return uint16(CRChi)<<8 | uint16(CRClo)
}

func calculateXOR(data []byte) byte {
	var crc byte = 0
	for _, b := range data {
		crc ^= b
	}
	return crc
}

func openPort(selectedPort string, openPortItem *widget.Button) {
	portMutex.Lock()
	defer portMutex.Unlock()

	if isPortOpen && currentPort != nil {
		log.Println("Закрытие предыдущего порта...")
		openPortItem.SetText("Открыть порт")
		closePort()
	}

	if selectedPort == "Нет доступных портов" {
		log.Println("Нет доступных портов")
		openPortItem.SetText("Открыть порт")
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
		openPortItem.SetText("Открыть порт")
		return
	}

	currentPort = port
	isPortOpen = true
	openPortItem.SetText("Закрыть порт")
	fmt.Println("Порт открыт:", selectedPort)

	stopChannel = make(chan bool)

	go readDataFromPort(port)
}

func closePort() {
	if currentPort != nil {
		if stopChannel != nil {
			close(stopChannel)
		}

		err := currentPort.Close()
		if err != nil {
			log.Println("Ошибка при закрытии порта:", err)
		} else {
			log.Println("Порт закрыт.")
		}

		currentPort = nil
		isPortOpen = false
	}
}

func (c *ConfigApp) ShowSettingsWindow(w fyne.Window) {
	settingsWindow := fyne.CurrentApp().NewWindow("Настройки")
	settingsWindow.Resize(fyne.NewSize(350, 400))

	label := widget.NewLabel("Настройки приложения")

	entry := widget.NewEntry()

	entry.OnChanged = func(text string) {
		if text == "" {
			return
		}
		num, err := strconv.Atoi(text)
		if err != nil || num < 1 || num > 100 { // Проверяем границы
			entry.SetText("")
		}
	}

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Кол-во строк по-умолчанию", Widget: entry}},
		OnSubmit: func() { // optional, handle form submission
			log.Println("Form submitted:", entry.Text)
			num, err := strconv.Atoi(entry.Text)
			if err == nil {
				c.quantityrow = num

				log.Println("Установлено количество строк:", c.quantityrow)
			} else {
				log.Println("Ошибка конвертации:", err)
			}

		},
	}

	closeButton := widget.NewButton("Закрыть", func() {
		settingsWindow.Close()
	})

	content := container.NewVBox(label, form, closeButton)
	settingsWindow.SetContent(content)

	settingsWindow.Show()
}

func main() {

	a := app.New()

	w := a.NewWindow("Привязка пульта к счетчику")

	config, _ := LoadConfig("config.json")

	w.Resize(fyne.NewSize(420, 800))
	w.SetFixedSize(true)

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
		go openPort(selectedPort, openPortItem)
	})

	settingsButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		config.ShowSettingsWindow(w)
	})

	topBar := container.NewHBox(
		portDropdown,
		openPortItem,
		settingsButton,
	)

	wrappedTopBar := container.NewGridWrap(fyne.NewSize(400, 40), topBar)

	entryList := container.NewVBox()

	headerRow := container.NewHBox(
		widget.NewLabelWithStyle("Номер счетчика", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Сетевой адрес", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Подсеть", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	)

	addEntryRow := func() {

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
		entryRows = append(entryRows, row)

		devNumContainer := container.NewGridWrap(fyne.NewSize(140, 40), devNum)
		rfNumContainer := container.NewGridWrap(fyne.NewSize(125, 40), rfNum)
		chNumContainer := container.NewGridWrap(fyne.NewSize(80, 40), chNum)
		checkContainer := container.NewGridWrap(fyne.NewSize(40, 40), check)

		entryRow := container.NewHBox(devNumContainer, rfNumContainer, chNumContainer, checkContainer)

		entryList.Add(entryRow)
		entryList.Refresh()
	}

	addRowButton := widget.NewButton("Добавить строку", func() {
		addEntryRow()
	})

	addRowButtonMust := widget.NewButton(fmt.Sprintf("Добавить %d", config.quantityrow), func() {
		for i := 0; i < config.quantityrow; i++ {
			addEntryRow()
		}
	})

	buttonRow := container.NewHBox(addRowButton, addRowButtonMust)

	scrollableEntryList := container.NewVScroll(entryList)
	scrollableEntryList.SetMinSize(fyne.NewSize(410, 650))

	mainContainer := container.NewVBox(buttonRow, headerRow, scrollableEntryList)

	w.SetContent(container.NewVBox(wrappedTopBar, mainContainer))

	for i := 0; i < 10; i++ {
		addEntryRow()
	}

	w.ShowAndRun()
}
