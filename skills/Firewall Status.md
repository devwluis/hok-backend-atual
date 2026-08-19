# Firewall Status

## Ação

```bash
ufw status 2>/dev/null || iptables -L -n --line-numbers 2>/dev/null | head -20
```
