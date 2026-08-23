# Simple request recording script

## Setup

Remove the demo origin from the repository-root `upstreams.txt`, then start the demo:

```sh
make build
make demo-simple
```

Close all existing Incognito windows, then open a new Incognito window and
load the web app URL printed by the demo. Press Command+Option+I to open
DevTools on the Network panel. Press
Command+Shift+5, choose **Record Entire Screen**, and click **Record**.

## Record

1. Click **Send simple request**. Show the API receiving the GET and the
   browser blocking its response.
2. Bring another terminal into view and start Seamless CORS:

   ```sh
   ./bin/seamless-cors start
   ```

3. Add the origin printed by the demo to the repository-root `upstreams.txt`:

   ```text
   http://api.127.0.0.1.nip.io:4100
   ```

4. Click **Retry the same request**. Show the successful response and repaired
   `Access-Control-Allow-Origin` header.
