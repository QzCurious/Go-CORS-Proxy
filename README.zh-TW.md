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

![Simple request demo](demo/cors-simple-requests/cors-simple-requests.gif)

### Preflighted request

瀏覽器發送 JSON `POST` request 前，會先送出 `OPTIONS` preflight request。

![Preflighted request demo](demo/cors-preflighted-requests/cors-preflighted-requests.gif)

## 安裝

### 使用 npm 安裝

將指令全域安裝：

```sh
npm install --global seamless-cors
```

### 使用 Go 安裝

若已安裝 Go 1.23.1 或更新版本，可建置並安裝最新發行版：

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

第一次啟動時，seamless-cors 會建立以下檔案：

- `~/.seamless-cors/config.yaml`
- `~/.seamless-cors/upstreams.txt`

把需要放行 CORS 的 API hostname 或 origin 逐行加進 `~/.seamless-cors/upstreams.txt`：

```text
api.dev.example.com
https://api.example.com:8443
*.test.example.com
```

服務執行期間會自動監看這份清單，修改後不需要重新啟動。

需要攔截 HTTPS 時，請在 `config.yaml` 將 `ca-trusted` 設為 `true`，再重新啟動服務。作業系統會要求你確認安裝 seamless-cors 的開發用 CA 憑證。

使用完畢後，按下 `Ctrl+C` 停止服務；seamless-cors 也會一併移除它設定的 PAC。

## 支援平台

目前會自動設定系統代理的版本以 macOS 與 Windows 為主。

## License

[MIT](LICENSE)
