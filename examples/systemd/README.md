# docker-ztd systemd restore service

This folder contains a `systemd` unit that restores all registered `docker-ztd` projects after host reboot.

## Files

- `docker-ztd-restore.service` — runs `docker ztd restore --all` after Docker is ready.

## Install

```bash
sudo cp examples/systemd/docker-ztd-restore.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now docker-ztd-restore.service
```

## Verify

```bash
sudo systemctl status docker-ztd-restore.service
journalctl -u docker-ztd-restore.service -b --no-pager
```

## Notes

- The service runs as `root` by default.
- If your `docker-ztd` plugin is installed for another user, adjust `User` and `Environment=HOME=...` in the unit file.
