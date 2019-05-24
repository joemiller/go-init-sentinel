go-init-sentinel
================

todo

Usage
-----

```sh
/go-init-sentinel -signal=HUP -watch=/certs/cert.pem -- /my-service -p 8080

# shutdown service with signal 15 TERM
/go-init-sentinel -signal=TERM -watch=/certs/cert.pem -- /my-service -p 8080

# multiple files - 1
/go-init-sentinel -signal=HUP -watch=/certs/cert.pem,/certs/cert.key,/another/file -- /my-service -p 8080

/go-init-sentinel -watch="/certs/cert.pem:1" -- /foo
/go-init-sentinel -watch="/certs/cert.pem:HUP" -- /foo
/go-init-sentinel -watch="/certs/cert.pem:HUP" -watch="/another/file:TERM" -- /foo
/go-init-sentinel -watch="/certs/cert.pem:1" -watch="/another/file:15" -- /foo
```

questions:
1. is it sufficient to send signals only to the direct child, or do we likely need to support sending the sig to all pids/children? maybe up to the other apps to manage their own children
2. stat() or inotify based watch? plan for needing both
3. 


```dockerfile
...
ENTRYPOINT ["/go-init-sentinel", "-watch=/certs/stunnel.pem:SIGHUP", "--"]
CMD ["/usr/bin/stunnel", "/config/stunnel.conf"]
```


Notes
-----

### kubernetes secretVol updates

inotify watches on kube secrets is difficult. Can't watch on the file itself, it's a symlink
and it will be replaced

```sh
$ kubectl exec wormhole-client-ttc4h -- ls -l /joe-test
drwxrwxrwt 3 root root  100 May 19 22:17 .
drwxr-xr-x 1 root root 4096 May 19 22:15 ..
drwxr-xr-x 2 root root   60 May 19 22:17 ..2019_05_19_22_17_34.047542780
lrwxrwxrwx 1 root root   31 May 19 22:17 ..data -> ..2019_05_19_22_17_34.047542780
lrwxrwxrwx 1 root root   10 May 19 22:15 foo -> ..data/foo

$ kubectl edit secret joe-test
$ date
Sun May 19 14:56:04 PDT 2019

2019/05/19 21:56:26 JOE: event: "/joe-test/..2019_05_19_21_56_26.122102519": CREATE
2019/05/19 21:56:26 JOE: event: "/joe-test/..2019_05_19_21_56_26.122102519": CHMOD
2019/05/19 21:56:26 JOE: event: "/joe-test/..data_tmp": RENAME
2019/05/19 21:56:26 JOE: event: "/joe-test/..data": CREATE
2019/05/19 21:56:26 JOE: event: "/joe-test/..2019_05_19_21_55_09.709462547": REMOVE
```

go reaper()

for each sentinel,
  start sentinel
  add to sentinel-error-channels
  add to sentinel-action-channels

cmd.start
for
  select
    signal channel:
      if sigchld, ignore
      else,
        forward to child
    sentinel-error-channels:
      log error
      shutdown
    sentinel-action-channels:
      send action (signal) to cmd.process.pid
