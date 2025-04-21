package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "strconv"
    "time"

    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/app"
    "fyne.io/fyne/v2/canvas"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/dialog"
    "fyne.io/fyne/v2/widget"

    "github.com/wcharczuk/go-chart"
)

// Data Structures

type HEXJSONEntry struct {
    CurrentDay     int     `json:"currentDay"`
    TshareRateHEX  float64 `json:"tshareRateHEX"`
    DailyPayoutHEX float64 `json:"dailyPayoutHEX"`
    PricePulseX    float64 `json:"pricePulseX"`
}

type HEXJSON []HEXJSONEntry // Define as a slice

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

const dateLayout = "02-01-2006" // DD-MM-YYYY

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

    activeBox := container.NewVBox()
    for i := range miners {
        if miners[i].Status != "completed" {
            matured, err := isMatured(miners[i].EndDate)
            if err != nil {
                continue
            }
            var entry fyne.CanvasObject
            if matured {
                endButton := widget.NewButton("END", func() {
                    dialog.ShowConfirm("Congratulations!", "Have you ended the mining contract and minted HEX?", func(yes bool) {
                        if yes {
                            miners[i].Status = "completed"
                            if err := saveMiners(miners); err != nil {
                                log.Println("Error saving miners:", err)
                            }
                            refreshTabs()
                        }
                    }, w)
                })
                // Use container.NewMax to constrain button size
                endButtonContainer := container.NewMax(endButton)
                endButtonContainer.Resize(fyne.NewSize(60, 30))

                label := widget.NewLabel(fmt.Sprintf("Miner: Start: %s, End: %s, T-Shares: %.2f (Matured)", miners[i].StartDate, miners[i].EndDate, miners[i].TShares))
                label.TextStyle = fyne.TextStyle{Bold: true}
                // Remove wrapping to prevent vertical character splitting
                label.Wrapping = fyne.TextWrapOff
                // Set label size to ensure sufficient width
                label.Resize(fyne.NewSize(300, 30))

                // Use HBox with centered alignment to keep button and label proportional
                entry = container.NewHBox(label, endButtonContainer)
            } else {
                days, _ := daysLeft(miners[i].EndDate)
                entry = widget.NewLabel(fmt.Sprintf("Miner: Start: %s, End: %s, T-Shares: %.2f (%d days left)", miners[i].StartDate, miners[i].EndDate, miners[i].TShares, days))
            }
            activeBox.Add(entry)
        }
    }

    // Button to open completed miners window
    completedMinersButton := widget.NewButton("View Completed Miners", func() {
        completedMiners := []Miner{}
        for _, miner := range miners {
            if miner.Status == "completed" {
                completedMiners = append(completedMiners, miner)
            }
        }

        // Create new window for completed miners
        completedWindow := fyne.CurrentApp().NewWindow("Completed Miners")
        completedWindow.Resize(fyne.NewSize(600, 400))

        if len(completedMiners) == 0 {
            completedWindow.SetContent(widget.NewLabel("No completed miners."))
            completedWindow.Show()
            return
        }

        // Pagination setup
        const itemsPerPage = 10
        totalPages := (len(completedMiners) + itemsPerPage - 1) / itemsPerPage
        currentPage := 1

        // Create container for miners
        minersBox := container.NewVBox()

        // Create page label
        pageLabel := widget.NewLabel(fmt.Sprintf("Page %d of %d", currentPage, totalPages))

        // Function to update displayed miners
        updateMiners := func() {
            minersBox.Objects = nil // Clear existing labels
            startIndex := (currentPage - 1) * itemsPerPage
            endIndex := startIndex + itemsPerPage
            if endIndex > len(completedMiners) {
                endIndex = len(completedMiners)
            }
            for i := startIndex; i < endIndex; i++ {
                miner := completedMiners[i]
                label := widget.NewLabel(fmt.Sprintf("Miner: Start: %s, End: %s, T-Shares: %.2f", miner.StartDate, miner.EndDate, miner.TShares))
                label.Wrapping = fyne.TextWrapOff // Prevent text wrapping
                minersBox.Add(label)
            }
            pageLabel.SetText(fmt.Sprintf("Page %d of %d", currentPage, totalPages))
            minersBox.Refresh()
        }

        // Create navigation buttons
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

        // Initial population of miners
        updateMiners()

        // Disable buttons as needed initially
        if currentPage == 1 {
            previousButton.Disable()
        }
        if currentPage == totalPages {
            nextButton.Disable()
        }

        // Navigation bar
        navBar := container.NewHBox(previousButton, pageLabel, nextButton)

        // Close button
        closeButton := widget.NewButton("Close", func() {
            completedWindow.Close()
        })

        // Layout the window
        content := container.NewVBox(
            widget.NewLabel("Completed Miners"),
            container.NewMax(minersBox), // Ensure minersBox takes available space
            navBar,
            closeButton,
        )

        completedWindow.SetContent(content)
        completedWindow.Show()
    })

    return container.NewVBox(totalLabel, widget.NewLabel("Active Miners"), activeBox, completedMinersButton)
}

func createLiveDataTab() fyne.CanvasObject {
    priceLabel := widget.NewLabel("HEX Price: $0.00")
    tsharePriceLabel := widget.NewLabel("T-Share Price: $0.00")
    tshareRateLabel := widget.NewLabel("T-Share Rate: 0")
    penaltiesLabel := widget.NewLabel("Penalties: 0")
    payoutLabel := widget.NewLabel("Payout Per T-Share: 0.0")
    beatLabel := widget.NewLabel("Gas: 0")

    container := container.NewVBox(
        priceLabel,
        tsharePriceLabel,
        tshareRateLabel,
        penaltiesLabel,
        payoutLabel,
        beatLabel,
    )

    updateFunc := func() {
        data, err := fetchLiveData()
        if err != nil {
            log.Println("Error fetching live data:", err)
            return
        }
        priceLabel.SetText(fmt.Sprintf("HEX Price: $%.2f", data.PricePulsechain))
        tsharePriceLabel.SetText(fmt.Sprintf("T-Share Price: $%.2f", data.TsharePricePulsechain))
        tshareRateLabel.SetText(fmt.Sprintf("T-Share Rate: %s", formatWithCommas(int(data.TshareRateHEXPulsechain))))
        penaltiesLabel.SetText(fmt.Sprintf("Penalties: %s", formatWithCommas(int(data.PenaltiesHEXPulsechain))))
        payoutLabel.SetText(fmt.Sprintf("Payout Per T-Share: %.1f", data.PayoutPerTsharePulsechain))
        beatLabel.SetText(fmt.Sprintf("Gas: %s beats", formatLongWithCommas(data.Beat)))
    }

    updateFunc() // Initial update

    go func() {
        for {
            time.Sleep(15 * time.Minute)
            updateFunc()
        }
    }()

    return container
}

func createChartTab() fyne.CanvasObject {
    selectField := widget.NewSelect([]string{"Price", "Rate", "Payout"}, nil)
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
            case "Price":
                graph.Series[0].(chart.ContinuousSeries).YValues[i] = entry.PricePulseX
            case "Rate":
                graph.Series[0].(chart.ContinuousSeries).YValues[i] = entry.TshareRateHEX
            case "Payout":
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
    updateChart("Price") // Default

    return container
}

func createSettingsTab(miners []Miner, w fyne.Window, refreshTabs func()) fyne.CanvasObject {
    startDateEntry := widget.NewEntry()
    startDateEntry.SetPlaceHolder("DD-MM-YYYY")
    endDateEntry := widget.NewEntry()
    endDateEntry.SetPlaceHolder("DD-MM-YYYY")
    tSharesEntry := widget.NewEntry()
    tSharesEntry.SetPlaceHolder("T-Shares")

    addButton := widget.NewButton("Add Miner", func() {
        startDate := startDateEntry.Text
        endDate := endDateEntry.Text
        tShares, err := strconv.ParseFloat(tSharesEntry.Text, 64)
        if err != nil {
            dialog.ShowError(fmt.Errorf("Invalid T-Shares: %v", err), w)
            return
        }
        _, err1 := time.Parse(dateLayout, startDate)
        _, err2 := time.Parse(dateLayout, endDate)
        if err1 != nil || err2 != nil {
            dialog.ShowError(fmt.Errorf("Invalid date format. Use DD-MM-YYYY"), w)
            return
        }
        newMiner := Miner{
            StartDate: startDate,
            EndDate:   endDate,
            TShares:   tShares,
        }
        miners = append(miners, newMiner)
        if err := saveMiners(miners); err != nil {
            log.Println("Error saving miners:", err)
        }
        refreshTabs()
    })

    minersList := container.NewVBox()
    for i := range miners {
        i := i // Capture range variable
        deleteButton := widget.NewButton("Delete", func() {
            dialog.ShowConfirm("Delete Miner", "Do you want to delete this HEX miner?", func(yes bool) {
                if yes {
                    miners = append(miners[:i], miners[i+1:]...)
                    if err := saveMiners(miners); err != nil {
                        log.Println("Error saving miners:", err)
                    }
                    refreshTabs()
                }
            }, w)
        })
        minerLabel := widget.NewLabel(fmt.Sprintf("Start: %s, End: %s, T-Shares: %.2f", miners[i].StartDate, miners[i].EndDate, miners[i].TShares))
        minersList.Add(container.NewHBox(minerLabel, deleteButton))
    }

    return container.NewVBox(
        widget.NewLabel("Add New Miner"),
        startDateEntry,
        endDateEntry,
        tSharesEntry,
        addButton,
        widget.NewLabel("Existing Miners"),
        minersList,
    )
}

// Main Function

func main() {
    // Initialize directories
    os.MkdirAll("data", 0755)
    os.MkdirAll("settings", 0755)

    // Update historical data
    if err := updateLocalHEXJSON(); err != nil {
        log.Println("Error updating local HEXJSON:", err)
    }

    // Load miners and handle initial setup
    miners, err := loadMiners()
    if err != nil {
        log.Println("Error loading miners:", err)
    }

    // GUI Setup
    a := app.New()
    w := a.NewWindow("HEXfetch")
    w.Resize(fyne.NewSize(800, 600))

    // Refresh function to update tabs
    var refreshTabs func()
    refreshTabs = func() {
        miners, _ = loadMiners() // Reload miners
        profileTab := container.NewTabItem("Profile", createProfileTab(miners, w, refreshTabs))
        liveDataTab := container.NewTabItem("Live Data", createLiveDataTab())
        chartTab := container.NewTabItem("Chart", createChartTab())
        settingsTab := container.NewTabItem("Settings", createSettingsTab(miners, w, refreshTabs))
        tabs := container.NewAppTabs(profileTab, liveDataTab, chartTab, settingsTab)
        w.SetContent(tabs)
    }

    // Initial tab setup
    refreshTabs()

    // Handle empty miners.json
    if len(miners) == 0 {
        dialog.ShowConfirm("Profile is Empty", "Profile is empty. Do you want to add HEX miners?", func(yes bool) {
            if yes {
                // If yes, user goes to Settings tab manually
            } else {
                miners = []Miner{{TShares: 0}}
                if err := saveMiners(miners); err != nil {
                    log.Println("Error saving initial miners:", err)
                }
                refreshTabs()
            }
        }, w)
    }

    w.ShowAndRun()
}
