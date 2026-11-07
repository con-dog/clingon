# Power
Check the battery, keep the Mac awake and put it to sleep

## Battery
Shows the battery's charge and whether it's charging.

```sh
pmset -g batt
```

## Keep awake
Stops your Mac sleeping until you press Ctrl-C.

```sh
caffeinate -dims
```

## Keep awake for a while
Stops your Mac sleeping for a set time.

```sh
caffeinate -dims -t $(( <minutes> * 60 ))
```

- `<minutes>` Minutes
  - (x) `60`

## Sleep now
> [!WARNING]
> Puts your Mac to sleep straight away.

```sh
pmset sleepnow
```
