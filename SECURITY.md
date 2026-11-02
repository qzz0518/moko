# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub:
[Report a vulnerability](https://github.com/qzz0518/moko/security/advisories/new)
(Security tab → "Report a vulnerability"). Do not open a public issue.

Include the Moko version, OS and steps to reproduce. You will usually get a reply within
a week; a fix ships in a new release, with credit if you want it.

Only the latest release is supported.

## What Moko talks to

- **Your air purifier**, over ECHONET Lite on UDP 3610 on your local network.
- **Your local network**, only when Apple Home (HomeKit) is turned on: a HomeKit
  accessory server advertised over Bonjour, which only paired Home devices can control.
- **GitHub**, to check for and download updates from this repository's releases over
  HTTPS. Updates are signed, and the app installs only those that verify against the
  public key built into it.
- **Open-Meteo** (`open-meteo.com`), for the weather, while it is turned on: it receives
  the coordinates of the place, rounded to about 100 m.
- **ipwho.is**, and **ipapi.co** if that fails, to estimate your location from your
  public IP address, only while the weather is on and you have not chosen a place.

There is no other network access, no telemetry and no account.

## In scope

For example: controlling Moko through HomeKit without pairing, getting an update
installed that was not signed with the release key, or privilege escalation through the
app, its updater or the Windows installer.

ECHONET Lite itself has no authentication: anything on the same network can control the
purifier, whether or not Moko is installed. That is not a Moko vulnerability.
