# Network
See Wi-Fi details, turn Wi-Fi on or off and flush the DNS cache

## Flush DNS cache
> [!WARNING]
> Forgets cached DNS answers, for when a site moved but your Mac still goes to the old address.

```sh
sudo dscacheutil -flushcache && sudo killall -HUP mDNSResponder
```

## Turn WiFi off
> [!WARNING]
> Switches Wi-Fi off.

```sh
networksetup -setairportpower <device> off
```

- `<device>` Which device

## Turn WiFi on
Switches Wi-Fi on.

```sh
networksetup -setairportpower <device> on
```

- `<device>` Which device

## WiFi details
Shows the details of your Wi-Fi connection and your address. MacOs may hide the network's name.

```sh
ipconfig getsummary <device>
```

- `<device>` Which device
