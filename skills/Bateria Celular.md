# Bateria Celular

## Ação

```bash
termux-battery-status 2>/dev/null || cat /sys/class/power_supply/battery/capacity 2>/dev/null
```
