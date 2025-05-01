package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "strconv"
    "sync"
    "time"

    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/app"
    "fyne.io/fyne/v2/canvas"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/dialog"
    "fyne.io/fyne/v2/widget"

    "github.com/wcharczuk/go-chart"
)

// Global variables for cached live data
var (
    latestLiveData LiveData
    liveDataMutex  sync.Mutex
)

// ConfigManager for thread-safe configuration
type ConfigManager struct {
    mu          sync.RWMutex
    config      Config
    changeChans []chan struct{} // Channels for frequency change notifications
}

var configManager = &ConfigManager{
    config: Config{LiveDataFrequency: defaultLiveDataFrequency},
}

func (cm *ConfigManager) GetLiveDataFrequency() int {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    return cm.config.LiveDataFrequency
}

func (cm *ConfigManager) SetLiveDataFrequency(frequency int) {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    cm.config.LiveDataFrequency = frequency
    log.Println("Set LiveDataFrequency to", frequency)
    // Notify all subscribers
    for i, ch := range cm.changeChans {
        select {
        case ch <- struct{}{}:
            log.Println("Sent frequency change signal to subscriber", i)
        default:
            log.Println("Warning: Frequency change channel full for subscriber", i)
        }
    }
}

func (cm *ConfigManager) Subscribe() chan struct{} {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    ch := make(chan struct{}, 1) // Buffered to avoid blocking
    cm.changeChans = append(cm.changeChans, ch)
    log.Println("New subscriber added, total subscribers:", len(cm.changeChans))
    return ch
}

// Data Structures
type HEXJSONEntry struct {
    CurrentDay     int     `json:"currentDay"`
    TshareRateHEX  float64 `json:"tshareRateHEX"`
    DailyPayoutHEX float64 `json:"dailyPayoutHEX"`
    PricePulseX    float64 `json:"pricePulseX"`
}

type HEXJSON []HEXJSONEntry

type LiveData struct {
    PricePulsechain           float64 `json:"price_Pulsechain"`
    TsharePricePulsechain     float64 `json:"tsharePrice_Pulsechain"`
    TshareRateHEXPulsechain   float64 `json:"tshareRateHEX_Pulsechain"`
    PenaltiesHEXPulsechain    float64 `json:"penaltiesHEX_Pulsechain"`
    PayoutPerTsharePulsechain float64 `json:"payoutPerTshare_Pulsechain"`
    Beat                      int64   `json:"beat"`
}

type Miner struct {
    StartDate string  `json:"startDate"`
    EndDate   string  `json:"endDate"`
    TShares   float64 `json:"tShares"`
    Status    string  `json:"status,omitempty"`
}

type Config struct {
    LiveDataFrequency int `json:"liveDataFrequency"`
}

const dateLayout = "02-01-2006" // DD-MM-YYYY for storage and display
const defaultLiveDataFrequency = 15 // Default frequency in minutes

// Custom CanvasObject for triggering updates
type updateTrigger struct {
    widget.BaseWidget
    onTapped func(*fyne.PointEvent)
}

type emptyRenderer struct {
    trigger *updateTrigger
}

func (e *emptyRenderer) Layout(_ fyne.Size) {}
func (e *emptyRenderer) MinSize() fyne.Size {
    return fyne.NewSize(0, 0)
}
func (e *emptyRenderer) Refresh() {}
func (e *emptyRenderer) ApplyTheme() {}
func (e *emptyRenderer) BackgroundColor() fyne.ThemeColorName {
    return "background"
}
func (e *emptyRenderer) Objects() []fyne.CanvasObject {
    return nil
}
func (e *emptyRenderer) Destroy() {}

func newUpdateTrigger() *updateTrigger {
    t := &updateTrigger{}
    t.ExtendBaseWidget(t)
    return t
}

func (t *updateTrigger) CreateRenderer() fyne.WidgetRenderer {
    return &emptyRenderer{trigger: t}
}

func (t *updateTrigger) Tapped(e *fyne.PointEvent) {
    if t.onTapped != nil {
        t.onTapped(e)
    }
}

func (t *updateTrigger) TappedSecondary(_ *fyne.PointEvent) {}

// Data Fetching and Management Functions
func fetchHEXJSON() (HEXJSON, error) {
    resp, err := http.Get("https://hexdailystats.com/fulldatapulsechain")
    if err != nil {
        return HEXJSON{}, err
    }
    defer resp.Body.Close()
    var data HEXJSON
    err = json.NewDecoder(resp.Body).Decode(&data)
    if err != nil {
        return HEXJSON{}, err
    }
    return data, nil
}

func fetchLiveData() (LiveData, error) {
    resp, err := http.Get("https://hexdailystats.com/livedata")
    if err != nil {
        return LiveData{}, err
    }
    defer resp.Body.Close()
    var data LiveData
    err = json.NewDecoder(resp.Body).Decode(&data)
    if err != nil {
        return LiveData{}, err
    }
    return data, nil
}

func loadLocalHEXJSON() (HEXJSON, error) {
    file, err := os.Open("data/hexjson.json")
    if err != nil {
        if os.IsNotExist(err) {
            return HEXJSON{}, nil
        }
        return HEXJSON{}, err
    }
    defer file.Close()
    var data HEXJSON
    err = json.NewDecoder(file).Decode(&data)
    if err != nil {
        return HEXJSON{}, err
    }
    return data, nil
}

func saveLocalHEXJSON(data HEXJSON) error {
    file, err := os.Create("data/hexjson.json")
    if err != nil {
        return err
    }
    defer file.Close()
    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ")
    return encoder.Encode(data)
}

func updateLocalHEXJSON() error {
    localData, err := loadLocalHEXJSON()
    if err != nil {
        return err
    }
    remoteData, err := fetchHEXJSON()
    if err != nil {
        return err
    }
    if len(localData) == 0 {
        return saveLocalHEXJSON(remoteData)
    }
    localMaxDay := localData[0].CurrentDay // Newest first
    var newEntries []HEXJSONEntry
    for _, entry := range remoteData {
        if entry.CurrentDay > localMaxDay {
            newEntries = append(newEntries, entry)
        } else {
            break // Sorted, so stop when we reach existing days
        }
    }
    if len(newEntries) > 0 {
        updatedData := append(newEntries, localData...)
        return saveLocalHEXJSON(updatedData)
    }
    return nil
}

func loadMiners() ([]Miner, error) {
    file, err := os.Open("settings/miners.json")
    if err != nil {
        if os.IsNotExist(err) {
            return []Miner{}, nil
        }
        return nil, err
    }
    defer file.Close()
    var miners []Miner
    err = json.NewDecoder(file).Decode(&miners)
    if err != nil {
        return nil, err
    }
    return miners, nil
}

func saveMiners(miners []Miner) error {
    file, err := os.Create("settings/miners.json")
    if err != nil {
        return err
    }
    defer file.Close()
    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ")
    return encoder.Encode(miners)
}

func loadConfig() (Config, error) {
    file, err := os.Open("settings/config.json")
    if err != nil {
        if os.IsNotExist(err) {
            return Config{LiveDataFrequency: defaultLiveDataFrequency}, nil
        }
        return Config{}, err
    }
    defer file.Close()
    var config Config
    err = json.NewDecoder(file).Decode(&config)
    if err != nil {
        return Config{}, err
    }
    if config.LiveDataFrequency <= 0 {
        config.LiveDataFrequency = defaultLiveDataFrequency
    }
    return config, nil
}

func saveConfig(config Config) error {
    file, err := os.Create("settings/config.json")
    if err != nil {
        return err
    }
    defer file.Close()
    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ")
    return encoder.Encode(config)
}

// Utility Functions
func isMatured(endDate string) (bool, error) {
    endTime, err := time.Parse(dateLayout, endDate)
    if err != nil {
        return false, err
    }
    now := time.Now()
    endDateOnly := time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 0, 0, 0, 0, endTime.Location())
    nowDateOnly := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
    return nowDateOnly.After(endDateOnly) || nowDateOnly.Equal(endDateOnly), nil
}

func daysLeft(endDate string) (int, error) {
    endTime, err := time.Parse(dateLayout, endDate)
    if err != nil {
        return 0, err
    }
    now := time.Now()
    endDateOnly := time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 0, 0, 0, 0, endTime.Location())
    nowDateOnly := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
    if nowDateOnly.After(endDateOnly) {
        return 0, nil
    }
    duration := endDateOnly.Sub(nowDateOnly)
    return int(duration.Hours() / 24), nil
}

func formatWithCommas(num int) string {
    str := strconv.Itoa(num)
    n := len(str)
    if n <= 3 {
        return str
    }
    var result []byte
    for i := 0; i < n; i++ {
        if i > 0 && (n-i)%3 == 0 {
            result = append(result, ',')
        }
        result = append(result, str[i])
    }
    return string(result)
}

func formatLongWithCommas(num int64) string {
    return formatWithCommas(int(num))
}

// GUI Creation Functions
func createProfileTab(miners []Miner, w fyne.Window, refreshTabs func()) fyne.CanvasObject {
    if len(miners) == 0 {
        return widget.NewLabel("Empty profile. Please add HEX miners in Settings")
    }

    totalTShares := 0.0
    for _, miner := range miners {
        if miner.Status != "completed" {
            totalTShares += miner.TShares
        }
    }
    totalLabel := widget.NewLabel(fmt.Sprintf("Total T-Shares: %.2f", totalTShares))

    // Create label for total value
    totalValueLabel := widget.NewLabel("Total T-Shares Value: $0.00")

    // Update total value label with initial data
    liveDataMutex.Lock()
    price := latestLiveData.TsharePricePulsechain
    liveDataMutex.Unlock()
    totalValueLabel.SetText(fmt.Sprintf("Total T-Shares Value: $%.2f", totalTShares*price))

    // Start a ticker to periodically update the total value label
    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        frequency := configManager.GetLiveDataFrequency()
        log.Println("Starting Profile tab ticker with frequency:", frequency, "minutes")
        ticker := time.NewTicker(time.Duration(frequency) * time.Minute)
        changeCh := configManager.Subscribe()
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                liveDataMutex.Lock()
                price := latestLiveData.TsharePricePulsechain
                liveDataMutex.Unlock()
                fyne.DoAndWait(func() {
                    totalValueLabel.SetText(fmt.Sprintf("Total T-Shares Value: $%.2f", totalTShares*price))
                    totalValueLabel.Refresh()
                })
                frequency = configManager.GetLiveDataFrequency()
                ticker.Reset(time.Duration(frequency) * time.Minute)
            case <-changeCh:
                frequency = configManager.GetLiveDataFrequency()
                log.Println("Profile tab ticker resetting to frequency:", frequency, "minutes")
                ticker.Reset(time.Duration(frequency) * time.Minute)
            case <-ctx.Done():
                log.Println("Profile tab ticker stopped")
                return
            }
        }
    }()

    // Stop the ticker when the app stops
    fyne.CurrentApp().Lifecycle().SetOnStopped(cancel)

    activeBox := container.NewVBox()
    for i := range miners {
        if miners[i].Status != "completed" {
            matured, err := isMatured(miners[i].EndDate)
            if err != nil {
                continue
            }
            var entry fyne.CanvasObject
            if matured {
                idx := i // Capture i for closure
                endButton := widget.NewButton("END", func() {
                    dialog.ShowConfirm("Congratulations!", "Have you ended the mining contract and minted HEX?", func(yes bool) {
                        if yes {
                            miners[idx].Status = "completed"
                            if err := saveMiners(miners); err != nil {
                                log.Println("Error saving miners:", err)
                            }
                            refreshTabs()
                        }
                    }, w)
                })
                endButtonContainer := container.NewMax(endButton)
                endButtonContainer.Resize(fyne.NewSize(60, 30))

                label := widget.NewLabel(fmt.Sprintf("Miner: Start: %s, End: %s, T-Shares: %.2f (Matured)", miners[i].StartDate, miners[i].EndDate, miners[i].TShares))
                label.TextStyle = fyne.TextStyle{Bold: true}
                label.Wrapping = fyne.TextWrapOff
                label.Resize(fyne.NewSize(300, 30))

                entry = container.NewHBox(label, endButtonContainer)
            } else {
                days, _ := daysLeft(miners[i].EndDate)
                entry = widget.NewLabel(fmt.Sprintf("Miner: Start: %s, End: %s, T-Shares: %.2f (%d days left)", miners[i].StartDate, miners[i].EndDate, miners[i].TShares, days))
            }
            activeBox.Add(entry)
        }
    }

    completedMinersButton := widget.NewButton("View Completed Miners", func() {
        completedMiners := []Miner{}
        for j := range miners {
            if miners[j].Status == "completed" {
                completedMiners = append(completedMiners, miners[j])
            }
        }

        completedWindow := fyne.CurrentApp().NewWindow("Completed Miners")
        completedWindow.Resize(fyne.NewSize(600, 400))

        if len(completedMiners) == 0 {
            completedWindow.SetContent(widget.NewLabel("No completed miners."))
            completedWindow.Show()
            return
        }

        const itemsPerPage = 10
        totalPages := (len(completedMiners) + itemsPerPage - 1) / itemsPerPage
        currentPage := 1

        minersBox := container.NewVBox()
        pageLabel := widget.NewLabel(fmt.Sprintf("Page %d of %d", currentPage, totalPages))

        updateMiners := func() {
            minersBox.Objects = nil
            startIndex := (currentPage - 1) * itemsPerPage
            endIndex := startIndex + itemsPerPage
            if endIndex > len(completedMiners) {
                endIndex = len(completedMiners)
            }
            for i := startIndex; i < endIndex; i++ {
                miner := completedMiners[i]
                label := widget.NewLabel(fmt.Sprintf("Miner: Start: %s, End: %s, T-Shares: %.2f", miner.StartDate, miner.EndDate, miner.TShares))
                label.Wrapping = fyne.TextWrapOff
                minersBox.Add(label)
            }
            pageLabel.SetText(fmt.Sprintf("Page %d of %d", currentPage, totalPages))
            minersBox.Refresh()
        }

        var previousButton, nextButton *widget.Button
        previousButton = widget.NewButton("Previous", func() {
            if currentPage > 1 {
                currentPage--
                updateMiners()
                if currentPage == 1 {
                    previousButton.Disable()
                }
                if currentPage < totalPages {
                    nextButton.Enable()
                }
            }
        })
        nextButton = widget.NewButton("Next", func() {
            if currentPage < totalPages {
                currentPage++
                updateMiners()
                if currentPage == totalPages {
                    nextButton.Disable()
                }
                if currentPage > 1 {
                    previousButton.Enable()
                }
            }
        })

        updateMiners()

        if currentPage == 1 {
            previousButton.Disable()
        }
        if currentPage == totalPages {
            nextButton.Disable()
        }

        navBar := container.NewHBox(previousButton, pageLabel, nextButton)
        closeButton := widget.NewButton("Close", func() {
            completedWindow.Close()
        })

        content := container.NewVBox(
            widget.NewLabel("Completed Miners"),
            container.NewMax(minersBox),
            navBar,
            closeButton,
        )

        completedWindow.SetContent(content)
        completedWindow.Show()
    })

    return container.NewVBox(totalLabel, totalValueLabel, widget.NewLabel("Active Miners"), activeBox, completedMinersButton)
}

func createLiveDataTab() fyne.CanvasObject {
    priceLabel := widget.NewLabel("Price: $0.00")
    tsharePriceLabel := widget.NewLabel("T-Share Price: $0.00")
    tshareRateLabel := widget.NewLabel("T-Share Rate: 0")
    penaltiesLabel := widget.NewLabel("Penalties: $0")
    payoutLabel := widget.NewLabel("Payout Per T-Share: 0.0")
    beatLabel := widget.NewLabel("Beat: 0")

    // Initial update
    liveDataMutex.Lock()
    data := latestLiveData
    liveDataMutex.Unlock()
    priceLabel.SetText(fmt.Sprintf("Price: $%.4f", data.PricePulsechain))
    tsharePriceLabel.SetText(fmt.Sprintf("T-Share Price: $%.2f", data.TsharePricePulsechain))
    tshareRateLabel.SetText(fmt.Sprintf("T-Share Rate: %s HEX", formatWithCommas(int(data.TshareRateHEXPulsechain))))
    penaltiesLabel.SetText(fmt.Sprintf("Penalties: %s HEX", formatWithCommas(int(data.PenaltiesHEXPulsechain))))
    payoutLabel.SetText(fmt.Sprintf("Payout Per T-Share: %.1f HEX", data.PayoutPerTsharePulsechain))
    beatLabel.SetText(fmt.Sprintf("Beat: %s", formatLongWithCommas(data.Beat)))

    // Start a ticker to periodically update the labels
    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        frequency := configManager.GetLiveDataFrequency()
        log.Println("Starting Live Data tab ticker with frequency:", frequency, "minutes")
        ticker := time.NewTicker(time.Duration(frequency) * time.Minute)
        changeCh := configManager.Subscribe()
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                liveDataMutex.Lock()
                data := latestLiveData
                liveDataMutex.Unlock()
                fyne.DoAndWait(func() {
                    priceLabel.SetText(fmt.Sprintf("Price: $%.4f", data.PricePulsechain))
                    tsharePriceLabel.SetText(fmt.Sprintf("T-Share Price: $%.2f", data.TsharePricePulsechain))
                    tshareRateLabel.SetText(fmt.Sprintf("T-Share Rate: %s HEX", formatWithCommas(int(data.TshareRateHEXPulsechain))))
                    penaltiesLabel.SetText(fmt.Sprintf("Penalties: %s HEX", formatWithCommas(int(data.PenaltiesHEXPulsechain))))
                    payoutLabel.SetText(fmt.Sprintf("Payout Per T-Share: %.1f HEX", data.PayoutPerTsharePulsechain))
                    beatLabel.SetText(fmt.Sprintf("Beat: %s", formatLongWithCommas(data.Beat)))
                    priceLabel.Refresh()
                    tsharePriceLabel.Refresh()
                    tshareRateLabel.Refresh()
                    penaltiesLabel.Refresh()
                    payoutLabel.Refresh()
                    beatLabel.Refresh()
                })
                frequency = configManager.GetLiveDataFrequency()
                ticker.Reset(time.Duration(frequency) * time.Minute)
            case <-changeCh:
                frequency = configManager.GetLiveDataFrequency()
                log.Println("Live Data tab ticker resetting to frequency:", frequency, "minutes")
                ticker.Reset(time.Duration(frequency) * time.Minute)
            case <-ctx.Done():
                log.Println("Live Data tab ticker stopped")
                return
            }
        }
    }()

    // Stop the ticker when the app stops
    fyne.CurrentApp().Lifecycle().SetOnStopped(cancel)

    return container.NewVBox(
        priceLabel,
        tsharePriceLabel,
        tshareRateLabel,
        penaltiesLabel,
        payoutLabel,
        beatLabel,
    )
}

func createChartTab() fyne.CanvasObject {
    selectField := widget.NewSelect([]string{"pricePulseX", "tshareRateHEX", "dailyPayoutHEX"}, nil)
    chartImage := canvas.NewImageFromFile("") // Placeholder
    chartImage.FillMode = canvas.ImageFillContain
    chartImage.SetMinSize(fyne.NewSize(600, 400))

    container := container.NewBorder(selectField, nil, nil, nil, chartImage)

    updateChart := func(field string) {
        data, err := loadLocalHEXJSON()
        if err != nil {
            log.Println("Error loading HEXJSON:", err)
            return
        }
        if len(data) == 0 {
            chartImage.Resource = nil
            chartImage.Refresh()
            return
        }
        graph := chart.Chart{
            XAxis: chart.XAxis{Name: "Current Day"},
            YAxis: chart.YAxis{Name: field},
            Series: []chart.Series{
                chart.ContinuousSeries{
                    XValues: make([]float64, len(data)),
                    YValues: make([]float64, len(data)),
                },
            },
        }
        for i, entry := range data {
            graph.Series[0].(chart.ContinuousSeries).XValues[i] = float64(entry.CurrentDay)
            switch field {
            case "pricePulseX":
                graph.Series[0].(chart.ContinuousSeries).YValues[i] = entry.PricePulseX
            case "tshareRateHEX":
                graph.Series[0].(chart.ContinuousSeries).YValues[i] = entry.TshareRateHEX
            case "dailyPayoutHEX":
                graph.Series[0].(chart.ContinuousSeries).YValues[i] = entry.DailyPayoutHEX
            }
        }
        buffer := bytes.NewBuffer(nil)
        err = graph.Render(chart.PNG, buffer)
        if err != nil {
            log.Println("Error rendering chart:", err)
            return
        }
        chartImage.Resource = fyne.NewStaticResource("chart", buffer.Bytes())
        chartImage.Refresh()
    }

    selectField.OnChanged = updateChart
    updateChart("pricePulseX") // Default

    return container
}

func createSettingsTab(miners []Miner, w fyne.Window, refreshTabs func()) fyne.CanvasObject {
    localMiners := miners
    startDateField := widget.NewEntry()
    startDateField.SetPlaceHolder("Click to select Start Date")
    startDateTap := widget.NewButton("", nil)
    startDateTap.Importance = widget.LowImportance
    startDateContainer := container.NewStack(startDateField, startDateTap)
    endDateField := widget.NewEntry()
    endDateField.SetPlaceHolder("Click to select End Date")
    endDateTap := widget.NewButton("", nil)
    endDateTap.Importance = widget.LowImportance
    endDateContainer := container.NewStack(endDateField, endDateTap)
    tSharesEntry := widget.NewEntry()
    tSharesEntry.SetPlaceHolder("T-Shares")

    tSharesEntry.Validator = func(s string) error {
        if s == "" {
            return fmt.Errorf("T-Shares is required")
        }
        val, err := strconv.ParseFloat(s, 64)
        if err != nil {
            return fmt.Errorf("T-Shares must be a valid number")
        }
        if val <= 0 {
            return fmt.Errorf("T-Shares must be positive number")
        }
        return nil
    }

    showCalendarDialog := func(title string, field *widget.Entry, w fyne.Window) {
        now := time.Now()
        selectedDate := now
        if field.Text != "" {
            if parsed, err := time.Parse(dateLayout, field.Text); err == nil {
                selectedDate = parsed
            }
        }

        years := make([]string, 0, 11)
        for y := 2020; y <= 2030; y++ {
            years = append(years, strconv.Itoa(y))
        }
        yearSelect := widget.NewSelect(years, nil)
        yearSelect.SetSelected(strconv.Itoa(selectedDate.Year()))

        months := []string{
            "January", "February", "March", "April", "May", "June",
            "July", "August", "September", "October", "November", "December",
        }
        monthSelect := widget.NewSelect(months, nil)
        monthSelect.SetSelected(months[selectedDate.Month()-1])

        days := make([]string, 0, 31)
        for d := 1; d <= 31; d++ {
            days = append(days, strconv.Itoa(d))
        }
        daySelect := widget.NewSelect(days, nil)
        daySelect.SetSelected(strconv.Itoa(selectedDate.Day()))

        form := &widget.Form{
            Items: []*widget.FormItem{
                {Text: "Year", Widget: yearSelect},
                {Text: "Month", Widget: monthSelect},
                {Text: "Day", Widget: daySelect},
            },
            SubmitText: "Confirm",
            CancelText: "Cancel",
        }

        d := dialog.NewCustomWithoutButtons(title, container.NewVBox(
            widget.NewLabel("Select Date"),
            form,
        ), w)
        form.OnSubmit = func() {
            year, _ := strconv.Atoi(yearSelect.Selected)
            monthIndex := 0
            for i, m := range months {
                if m == monthSelect.Selected {
                    monthIndex = i + 1
                    break
                }
            }
            day, _ := strconv.Atoi(daySelect.Selected)

            date, err := time.Parse("2006-1-2", fmt.Sprintf("%d-%d-%d", year, monthIndex, day))
            if err != nil {
                dialog.ShowError(fmt.Errorf("Invalid date: %s %s, %s", monthSelect.Selected, daySelect.Selected, yearSelect.Selected), w)
                return
            }

            field.SetText(date.Format(dateLayout))
            field.Refresh()
            d.Hide()
        }
        form.OnCancel = func() {
            d.Hide()
        }
        d.Show()
    }

    startDateTap.OnTapped = func() {
        showCalendarDialog("Select Start Date", startDateField, w)
    }
    startDateField.OnSubmitted = func(_ string) {
        showCalendarDialog("Select Start Date", startDateField, w)
    }
    endDateTap.OnTapped = func() {
        showCalendarDialog("Select End Date", endDateField, w)
    }
    endDateField.OnSubmitted = func(_ string) {
        showCalendarDialog("Select End Date", endDateField, w)
    }

    addButton := widget.NewButton("Add Miner", func() {
        if startDateField.Text == "" {
            dialog.ShowError(fmt.Errorf("Start date is required"), w)
            return
        }
        if endDateField.Text == "" {
            dialog.ShowError(fmt.Errorf("End date is required"), w)
            return
        }
        if _, err := time.Parse(dateLayout, startDateField.Text); err != nil {
            dialog.ShowError(fmt.Errorf("Invalid start date format"), w)
            return
        }
        if _, err := time.Parse(dateLayout, endDateField.Text); err != nil {
            dialog.ShowError(fmt.Errorf("Invalid end date format"), w)
            return
        }
        if tSharesEntry.Text == "" {
            dialog.ShowError(fmt.Errorf("T-Shares is required"), w)
            return
        }
        if err := tSharesEntry.Validate(); err != nil {
            dialog.ShowError(err, w)
            return
        }
        tShares, err := strconv.ParseFloat(tSharesEntry.Text, 64)
        if err != nil {
            dialog.ShowError(fmt.Errorf("Invalid T-Shares: %v", err), w)
            return
        }
        newMiner := Miner{
            StartDate: startDateField.Text,
            EndDate:   endDateField.Text,
            TShares:   tShares,
        }
        localMiners = append(localMiners, newMiner)
        if err := saveMiners(localMiners); err != nil {
            log.Println("Error saving miners:", err)
        }
        refreshTabs()
    })

    frequencyEntry := widget.NewEntry()
    frequencyEntry.SetPlaceHolder("Live Data Update Frequency (minutes)")
    frequencyEntry.SetText(fmt.Sprintf("%d", configManager.GetLiveDataFrequency()))

    saveFrequencyButton := widget.NewButton("Save Frequency", func() {
        frequency, err := strconv.Atoi(frequencyEntry.Text)
        if err != nil || frequency <= 0 {
            dialog.ShowError(fmt.Errorf("Frequency must be a positive integer"), w)
            return
        }
        config := Config{LiveDataFrequency: frequency}
        if err := saveConfig(config); err != nil {
            log.Println("Error saving config:", err)
            dialog.ShowError(fmt.Errorf("Failed to save frequency"), w)
            return
        }
        configManager.SetLiveDataFrequency(frequency)
        dialog.ShowInformation("Success", fmt.Sprintf("Live data update frequency set to %d minutes", frequency), w)
    })

    minersList := container.NewVBox()
    for i := range localMiners {
        idx := i // Capture i for closure
        deleteButton := widget.NewButton("Delete", func() {
            dialog.ShowConfirm("Delete Miner", "Do you want to delete this HEX miner?", func(yes bool) {
                if yes {
                    localMiners = append(localMiners[:idx], localMiners[idx+1:]...)
                    if err := saveMiners(localMiners); err != nil {
                        log.Println("Error saving miners:", err)
                    }
                    refreshTabs()
                }
            }, w)
        })
        minerLabel := widget.NewLabel(fmt.Sprintf("Start: %s, End: %s, T-Shares: %.2f", localMiners[i].StartDate, localMiners[i].EndDate, localMiners[i].TShares))
        minersList.Add(container.NewHBox(minerLabel, deleteButton))
    }

    return container.NewVBox(
        widget.NewLabel("Live Data Settings"),
        frequencyEntry,
        saveFrequencyButton,
        widget.NewLabel("Add New Miner"),
        startDateContainer,
        endDateContainer,
        tSharesEntry,
        addButton,
        widget.NewLabel("Existing Miners"),
        minersList,
    )
}

// Main Function
func main() {
    os.MkdirAll("data", 0755)
    os.MkdirAll("settings", 0755)

    if err := updateLocalHEXJSON(); err != nil {
        log.Println("Error updating local HEXJSON:", err)
    }

    miners, err := loadMiners()
    if err != nil {
        log.Println("Error loading miners:", err)
    }

    // Load initial config and set in configManager
    config, err := loadConfig()
    if err != nil {
        log.Println("Error loading config:", err)
        config.LiveDataFrequency = defaultLiveDataFrequency
    }
    configManager.SetLiveDataFrequency(config.LiveDataFrequency)

    // Initial fetch of live data at startup
    data, err := fetchLiveData()
    if err != nil {
        log.Println("Error during initial live data fetch:", err)
    } else {
        liveDataMutex.Lock()
        latestLiveData = data
        liveDataMutex.Unlock()
    }

    // Start periodic live data fetching
    go func() {
        frequency := configManager.GetLiveDataFrequency()
        log.Println("Starting live data fetch ticker with frequency:", frequency, "minutes")
        ticker := time.NewTicker(time.Duration(frequency) * time.Minute)
        changeCh := configManager.Subscribe()
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                data, err := fetchLiveData()
                if err != nil {
                    log.Println("Error fetching live data:", err)
                } else {
                    liveDataMutex.Lock()
                    latestLiveData = data
                    liveDataMutex.Unlock()
                    log.Println("Updated latestLiveData with TsharePricePulsechain:", latestLiveData.TsharePricePulsechain)
                }
                frequency = configManager.GetLiveDataFrequency()
                ticker.Reset(time.Duration(frequency) * time.Minute)
            case <-changeCh:
                frequency = configManager.GetLiveDataFrequency()
                log.Println("Live data fetch ticker resetting to frequency:", frequency, "minutes")
                ticker.Reset(time.Duration(frequency) * time.Minute)
            }
        }
    }()

    a := app.New()
    w := a.NewWindow("HEX Stats")
    w.Resize(fyne.NewSize(800, 600))

    var refreshTabs func()
    refreshTabs = func() {
        log.Println("Refreshing tabs")
        miners, _ = loadMiners()
        profileTab := container.NewTabItem("Profile", createProfileTab(miners, w, refreshTabs))
        liveDataTab := container.NewTabItem("Live Data", createLiveDataTab())
        //chartTab := container.NewTabItem("Chart", createChartTab())
        settingsTab := container.NewTabItem("Settings", createSettingsTab(miners, w, refreshTabs))
        tabs := container.NewAppTabs(profileTab, liveDataTab, settingsTab) // chartTab
        w.SetContent(tabs)
    }

    refreshTabs()
    w.ShowAndRun()
}
