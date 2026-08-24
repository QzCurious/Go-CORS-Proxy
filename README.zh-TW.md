# seamless-cors

[English](README.md) | 繁體中文

`seamless-cors` 是一套給本機開發與 QA 使用的工具，讓瀏覽器可以直接對不同來源的 API 發送請求，不必修改前端原本使用的 URL，也不必為了測試去調整 API 伺服器的 CORS 設定。

它會透過 Proxy Auto-Configuration（PAC）檔案，只把你指定的網域交給本機代理服務處理。HTTP 開箱即可使用；需要測試 HTTPS 時，也可以另外啟用攔截功能。

> [!NOTE]
> 此專案仍在持續開發中。

> [!WARNING]
> seamless-cors 僅供本機開發與 QA 使用，請勿用於正式環境。

## Demo

### Simple request

瀏覽器直接對沒有回傳 CORS headers 的 API 發送跨來源 `GET` request。

![Simple request demo](https://raw.githubusercontent.com/QzCurious/seamless-cors/HEAD/demo/cors-simple-requests/cors-simple-requests.gif)

### Preflighted request

瀏覽器發送 JSON `POST` request 前，會先送出 `OPTIONS` preflight request。

![Preflighted request demo](https://raw.githubusercontent.com/QzCurious/seamless-cors/HEAD/demo/cors-preflighted-requests/cors-preflighted-requests.gif)

## 安裝

### 使用 npm 安裝

將指令全域安裝：

```sh
npm install --global seamless-cors
```

### 使用 Go 安裝

若已安裝 Go 1.27.0 或更新版本，可建置並安裝最新發行版：

```sh
go install github.com/QzCurious/seamless-cors/cmd/seamless-cors@latest
```

請確認 Go 的執行檔目錄（`$(go env GOPATH)/bin`）已加入 `PATH`。

## 快速開始

不必全域安裝，即可直接透過 `npx` 執行：

```sh
npx seamless-cors start
```

若已安裝，也可直接執行指令：

```sh
seamless-cors start
```

第一次啟動時，seamless-cors 會詢問是否要在平台原生的 XDG 設定目錄下建立
Global Upstream List：`seamless-cors/upstreams.txt`。例如 Linux 預設位置是
`~/.config/seamless-cors/upstreams.txt`，macOS 則是
`~/Library/Application Support/seamless-cors/upstreams.txt`。舊的
`~/.seamless-cors/upstreams.txt` 不會被讀取或搬移。

把需要放行 CORS 的 API hostname 或 origin 逐行加進 Global Upstream List：

```text
api.dev.example.com
https://api.example.com:8443
*.test.example.com
```

也可以在執行 `seamless-cors start` 的目錄直接建立選用的 `upstreams.txt`。
Global 與 Directory Upstream List 會各自被監看與解析，再以不具優先順序的方式
合併；重複的正規化項目只會生效一次。作用中的目錄會固定到 gateway 停止並重新啟動為止。

任一來源中明確的 `https://` Origin Selector 都代表 HTTPS Intent；Host Selector
本身不代表 HTTPS Intent，但 HTTPS Readiness 為 ready 時會同時處理 HTTP 與 HTTPS。

要讓 HTTPS Readiness 成為 ready，請執行 `seamless-cors install`，並在作業系統提示中允許安裝 seamless-cors 的開發用 CA 憑證。若 gateway 已在執行，HTTPS 攔截會立即啟用。未安裝 UserCA 時，gateway 仍會繼續處理 HTTP；若 upstream list 表達 HTTPS Intent，則會顯示警告。

再次執行 `seamless-cors install` 可在不中斷 HTTPS 流量的情況下更新
UserCA。`seamless-cors uninstall` 會移除所有 seamless-cors UserCA；若
HTTPS 攔截正在啟用，命令會先要求確認，確認後立即停用 HTTPS，而
gateway 仍會繼續處理 HTTP。

使用完畢後，按下 `Ctrl+C` 停止服務；seamless-cors 也會一併移除它設定的 PAC。

## 儲存位置

| 用途                              | 位置                    |
| --------------------------------- | ----------------------- |
| Global Upstream List              | XDG 設定目錄             |
| Installed UserCA material         | XDG state 目錄           |
| Gateway lock 與 discovery state   | XDG runtime 目錄         |
| Directory Upstream List           | Start 工作目錄           |

若從使用 `~/.seamless-cors/runtime` 的舊版本升級，請先停止舊 gateway。新版不會
讀取該目錄，也不會搬移其中的 lock 或 discovery 檔案；舊程序停止後，可手動移除
殘留的舊檔案。請勿同時執行新舊版本，因為兩者使用不同 lock 路徑，可能繞過互斥保護。

## 支援平台

目前會自動設定系統代理的版本以 macOS 與 Windows 為主。

## License

[MIT](LICENSE)
