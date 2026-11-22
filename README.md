# Debt Reducer

Debt Reducer works out the fastest and cheapest way to pay off your debts,
then helps you stick to the plan month by month.

Add your credit cards, loans and other balances, choose how to pay them down,
and it lays out every monthly payment until you're debt free: what each debt
gets, how much interest you pay, and when each one is paid off.

- **Plan:** pick a strategy and pay your minimums, or your minimums plus an
  extra amount each month. Each month shows what to pay on every debt, with
  the extra listed separately so you know exactly where it goes. Mark each
  month Paid or Skipped as you go, and the rest of the plan adjusts.
- **Simulate:** try a one-time payment (a bonus or tax refund) or a bigger
  monthly payment, and see how much interest and time it saves before you
  add it to your plan.
- **Debts:** keep every balance in one place, including promotional 0% APR
  purchases. Pause a debt to leave it out of the plan without deleting it.
- **Private:** everything stays on your computer. There's no account, and
  nothing is sent anywhere.

| Strategy      | Extra money goes to       | Best for                  |
|---------------|---------------------------|---------------------------|
| Avalanche     | Highest interest rate     | Paying the least interest |
| Snowball      | Smallest balance          | Quick wins                |
| Minimums only | Nothing extra             | Seeing what you'd save    |

When a debt is paid off, its minimum payment rolls into the next one.

## Install

Download the latest version from the
[Releases page](https://github.com/lee5i3/debt-reducer/releases).

### Windows

1. Download `Debt-Reducer-Setup-<version>-x64.exe`.
2. Run it and follow the steps. You can choose where to install it, and it
   adds Debt Reducer to the Start Menu and the desktop.

Windows will show "Windows protected your PC", because the installer isn't
signed yet. Choose **More info**, then **Run anyway**.

### Linux

1. Download `Debt-Reducer-<version>-x86_64.AppImage`.
2. Make it executable and run it:

   ```sh
   chmod +x Debt-Reducer-*.AppImage
   ./Debt-Reducer-*.AppImage
   ```

   Or right-click the file, open **Properties**, allow it to run as a
   program, then double-click it.

AppImages need FUSE 2. If it doesn't start, install it (Ubuntu and Debian:
`sudo apt install libfuse2`, or `libfuse2t64` on Ubuntu 24.04 and later).

### macOS

1. Download the `.dmg` for your Mac: `Debt-Reducer-<version>-arm64.dmg` for
   Apple Silicon (M1 and later), or `Debt-Reducer-<version>-x64.dmg` for
   Intel. (Apple menu › About This Mac shows which you have.)
2. Open it and drag **Debt Reducer** into **Applications**.

The app isn't notarized by Apple yet, so the first time you open it macOS
says it can't verify the developer. Open **System Settings › Privacy &
Security**, scroll down, and choose **Open Anyway** next to Debt Reducer. If
macOS says the app is damaged, run
`xattr -cr "/Applications/Debt Reducer.app"` in Terminal, then open it again.

## Your data

Your debts and plan are saved in a single file on your computer:

| System  | Folder                                       |
|---------|----------------------------------------------|
| Windows | `%APPDATA%\Debt Reducer`                     |
| Linux   | `~/.config/Debt Reducer`                     |
| macOS   | `~/Library/Application Support/Debt Reducer` |

To keep it somewhere else, for example a Dropbox, OneDrive or iCloud Drive
folder as a backup, open **Settings › Data** and choose **Change folder**.
Debt Reducer moves your data there. If the folder already has Debt Reducer
data, you can use that data instead, or replace it (the old file is kept as a
backup).

## Update

Download and install the new version the same way. Your data stays where it
is and is updated automatically the first time the new version opens.

## Uninstall

- **Windows:** Settings › Apps › Installed apps › Debt Reducer › Uninstall.
- **macOS:** drag Debt Reducer from Applications to the Trash.
- **Linux:** delete the AppImage.

Uninstalling doesn't delete your data. To remove it too, delete the data
folder above (or the folder you chose in Settings).

## Build from source

You need [Go](https://go.dev/dl/) (see `go.mod` for the version),
[Node.js](https://nodejs.org/) 24 and `make`.

```sh
git clone https://github.com/lee5i3/debt-reducer.git
cd debt-reducer
make init            # download dependencies
make desktop         # build and open the desktop app
```

To build an installer instead:

```sh
make desktop-dist PLATFORMS="--linux AppImage" ARCHS="--x64"   # Linux AppImage
make desktop-dist PLATFORMS="--mac dmg" ARCHS="--arm64"        # macOS (on a Mac)
```

The result is in `bin/dist/`. You can also run it as a web app in your
browser with `make run` (then open http://localhost:8080).

For development (tests, live reload, project layout, the JSON API), see
[DEVELOPMENT.md](DEVELOPMENT.md).
