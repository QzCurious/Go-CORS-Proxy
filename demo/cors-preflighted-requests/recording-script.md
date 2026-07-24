# Preflighted request recording script

## Setup

Remove the demo origin from `~/.seamless-cors/domains.txt`, then start the demo:

```sh
make build
make demo-preflight
```

Close all existing Incognito windows, then open a new Incognito window and
load the web app URL printed by the demo. Press Command+Option+I to open
DevTools on the Network panel. Press Command+Shift+5, choose **Record Entire
Screen**, and click **Record**.

## Record

1. Click **Send JSON request**. Show the API receiving `OPTIONS`, returning
   `405`, and never receiving `POST`.
2. Bring another terminal into view and start Seamless CORS:

   ```sh
   ./bin/seamless-cors start
   ```

3. Add the origin printed by the demo to `~/.seamless-cors/domains.txt`:

   ```text
   http://api.127.0.0.1.nip.io:4100
   ```

4. Click **Retry the same request**. Show Seamless CORS returning `204` for the
   preflight and the API receiving the JSON `POST`.
