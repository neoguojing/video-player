### open window
```shell
curl --location 'http://localhost:8080/open-window' \
--header 'Content-Type: application/json' \
--data '{
    "wsurl": "wss://10.9.19.159:30443/engine/rtsp-preview/rtsp-over-ws?jwt=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJ1SzZRZFVabUxDQTlrSGxhRElRNEZ1UE0iLCJuYmYiOjE3MDQ3OTU5MDIsImV4cCI6MTcwNDgwMzEwMn0.pvI4NNzOTLXKn1xHdjU5NYjJaoK_JcpfwZhfzBbw9r0",
    "rtspurl": "rtsp://10.224.0.38:554/10002024010808530434101",
    "x": 50,
    "y": 50,
    "width": 1080,
    "height": 720
}'
```

### move window

```shell
curl --location 'http://localhost:8080/move-window/window1' \
--header 'Content-Type: application/json' \
--data '{
    "x": 200,
    "y": 200,
    "width": 720,
    "height": 360
}'
```
### close window
```shell
curl --location --request POST 'http://localhost:8080/close-window/window1'
```