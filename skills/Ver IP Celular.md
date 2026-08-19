# Ver IP Celular

## Ação

```bash
ip addr show wlan0 2>/dev/null | grep inet || termux-wifi-connectioninfo 2>/dev/null
```
