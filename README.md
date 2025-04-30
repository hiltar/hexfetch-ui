# hexfetch-ui

hexfetch-ui is a GUI for hexfetch with convenient features like HEX miners and periodical data fetching from Pulsechain API.   
This doesn't itself interact Pulsechain network but instead it uses HEXDailyStats API to fetch data.   
UI doesn't need the 0x addresses at all so it's 100% privacy.

hexfetch-ui is made with `go 1.24.2` and ``fyne v1.4.3``.   
Fyne v2.6.0 isn't working with Debian 12 and go 1.24.2 due to missing driver in Fyne's desktop utility. Perhaps it will work with Debian 13 Trixie or Ubuntu.

Running hexfetch-ui will create two folders into same directory where hexfetch-ui is running.   
data directory contains hexdata.json.  
settings directory contains user defined config.json and miners.json.

## Upcoming features
Charts tab is disabled in the code because it's not ready.   
For now, it only shows chart as image without any functions.

Add pagination for Active Miners and Existing Miners.

Update the `Total T-Shares Value` in Profile tab to be updated periodically from Live Data.

---

# Build
```
go mod init hexfetch-ui
go mod edit -require fyne.io/fyne@v1.4.3
go mod edit -droprequire fyne.io/fyne/v2
go mod tidy
go build -o hexfecth-ui

./hexfetch-ui
```
---

# Tabs

## Profile
Profile tab shows user's miners and T-Shares and total value of T-Shares.   
If miner is matured, it will be shown **(MATURED)** with `END` button. Ending the miner will move it into `Completed Miners` container.

![Profile tab](https://github.com/user-attachments/assets/fa38be4d-b562-4b04-8eeb-4f72059534ed)   

Viewing Completed Miners button opens a window of completed HEX miners.

![Completed Miners](https://github.com/user-attachments/assets/320e3c1c-4946-4cb0-9c4f-d320369ebb98)


## Live Data
Live Data tab shows periodically fetched data from Pulsechain API.

![Live Data tab](https://github.com/user-attachments/assets/d8cbe7d5-4343-427a-a190-483e9370abf2)


## Settings
Settings tab shows:  
  - Live Data Settings for changing the frequency of fetching data (in minutes)  
  - Add New Miner for adding HEX miner with start date, end date and amount of T-Shares  
  - Existing Miners for list of HEX miners with Delete function  

![image](https://github.com/user-attachments/assets/93a7477c-cbb9-4523-8081-5c4eb236aa34)
