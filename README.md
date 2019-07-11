go-init-sentinel
================

A minimal init system capable of watching files for changes and sending signals for reload, shutdown, etc.

`go-init-sentinel` is intended as a wrapper for processes that need to be notified when a
configuration file or TLS certificate is updated by an external process. For example, a wrapped
nginx process can be sent a SIGHUP to reload configuration including TLS certificates.

Example use cases:

- An app running in Kubernetes with a sidecar that is updating TLS certificates from Vault can use
  `go-init-sentinel` to detect updates and signal the app to reload the certificate.
- An external process fetching new certificates and updating a Kubernetes SecretVolume mount.
  `go-init-sentinel` can detect updates to the mounted volume and signal the app to reload.

Suitable for use as pid-1 in a docker container. go-init-sentinel will forward signals to the
child process.

A simple `stat()`-based polling mechanism is used to detect file changes. The default interval
is 10 seconds and may be customized via the `-interval` flag. `stat()` is used as the change
detection method because experimentation showed that real-time notification interfaces such as
inotify had a variety of edge cases and was dependent on the method in which files were updated.
A simple "stat on interval" approach is simple, reliable, and works in a variety of cases including
with Kubernetes SecretVolume and ConfigMap mounts.

Install
-------

- Binary releases are available for multiple platforms on [GitHub release](https://github.com/joemiller/go-init-sentinel/releases) page.
- Docker images are also available.

Install using multi-stage Docker build:

```dockerfile
FROM joemiller/go-init-sentinel as go-init-sentinel
...

COPY --from=go-init-sentinel /go-init-sentinel /go-init-sentinel
...
```

Usage
-----

Typical usage is via Docker `ENTYRPOINT`:

Watch a certificate for updates, send SIGHUP on change:

```dockerfile
...
ENTRYPOINT ["/go-init-sentinel", "-watch=/certs/stunnel.pem:SIGHUP", "--"]
CMD ["/usr/bin/stunnel", "/config/stunnel.conf"]
```

The `-watch` flag may be specified multiple tiems to watch multiple files. Each file
change may send a different signal.

```dockerfile
...
ENTRYPOINT ["/go-init-sentinel",
            "-watch=/etc/nginx/nginx.conf:SIGHUP",
            "-watch=/certs/tls.pem:SIGHUP",
            "--"]
CMD ["/usr/bin/nginx"]
```

A random amount of time can be added to the check interval by adding the `-splay` flag.
This can be useful if you have many instances (pods) where the reload operation may be slow
and you want to avoid all instances being unavailable at the same time. The splay time added to
the interval will be between `"0-<interval>"` seconds.
