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

![Simple request demo](demo/cors-simple-requests/cors-simple-requests.mp4)

### Preflighted request

瀏覽器發送 JSON `POST` request 前，會先送出 `OPTIONS` preflight request。

![Preflighted request demo](demo/cors-preflighted-requests/cors-preflighted-requests.mp4)

## 安裝

### 使用 Go 安裝

若已安裝 Go 1.23.1 或更新版本，可建置並安裝最新發行版：

```sh
go install github.com/QzCurious/seamless-cors/cmd/seamless-cors@latest
```

請確認 Go 的執行檔目錄（`$(go env GOPATH)/bin`）已加入 `PATH`。

### 使用預先建置的發行版

前往 [最新的 GitHub Release](https://github.com/QzCurious/seamless-cors/releases/latest)，下載符合你的作業系統與 CPU 架構的壓縮檔並解壓縮。

#### macOS

將壓縮檔在 `~/Downloads` 解壓縮後，執行以下指令安裝：

```sh
mkdir -p ~/.local/bin
mv ~/Downloads/seamless-cors ~/.local/bin/seamless-cors
# 允許從可信任 GitHub Release 下載的版本在 macOS 上執行
xattr -d com.apple.quarantine ~/.local/bin/seamless-cors
```

請確認你使用的 shell 已將 `~/.local/bin` 加入 `PATH`。

#### Windows

將解壓縮後的 `seamless-cors.exe` 移到你擁有的目錄，例如
`%USERPROFILE%\.local\bin`。在 Windows 的環境變數中，將該目錄加入使用者的
`Path`，然後開啟新的終端機。

未簽章的 Release 可能會出現 Windows SmartScreen 警告。若你確認下載來源是本
專案的 GitHub Release，請選擇 **更多資訊**，再選擇 **仍要執行**；受管理的工作
裝置可能不允許這個例外。

## 快速開始

```sh
seamless-cors start
```

第一次啟動時，seamless-cors 會建立以下檔案：

- `~/.seamless-cors/config.yaml`
- `~/.seamless-cors/domains.txt`

把需要放行 CORS 的 API hostname 或 origin 逐行加進 `~/.seamless-cors/domains.txt`：

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
