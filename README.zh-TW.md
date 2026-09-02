# seamless-cors

[English](README.md) | 繁體中文

讓 CORS 成為開發環境的問題，而不是應用程式的問題。`seamless-cors` 透過 PAC 自動將指定請求導流到本機 CORS proxy，讓前端維持 API URL 與請求邏輯，API Server 也不需要為測試環境額外配置 CORS。

> [!NOTE]
> 此專案仍在積極開發中。

> [!WARNING]
> seamless-cors 僅供本機開發與 QA 環境使用。

## 快速開始

直接透過 `npx` 啟動 seamless-cors：

```sh
npx seamless-cors start
```

第一次執行時，請依提示建立 Global Upstream List，接著在指令顯示的路徑中，逐行加入要處理的 hostname 或 origin：

```text
api.dev.example.com
http://localhost:3000
https://api.example.com:8443
```

儲存檔案後，重新發送一次瀏覽器請求即可。seamless-cors 執行期間會自動套用清單變更，不需要重新啟動。

`start` 會持續在目前的終端機前景執行，並暫時替適用的系統網路服務設定 PAC URL。若網路服務原本使用其他 PAC 設定，seamless-cors 會保持原狀，不會覆蓋。使用完畢後按下 `Ctrl+C`，即可停止服務，並移除 seamless-cors 加上的 PAC 設定。

流量執行環境會先啟動，再進行 System PAC delivery。PAC 探索、讀取、寫入或驗證問題只會回報，不會停止已啟動的執行環境；之後有效的路由變更或再次執行 `start`，都會觸發一次新的 delivery。

若要處理原本就是 HTTPS 的 API，或使用 HTTPS Facade，請安裝開發用 CA，並同意作業系統顯示的授權提示：

```sh
npx seamless-cors install
```

若 seamless-cors 已在執行，符合清單設定的 HTTPS 網址會立即生效。停止服務後，CA 仍會保留在系統中；若要移除，請另外執行 `npx seamless-cors uninstall`。

## 示範

### 簡單請求

瀏覽器直接向未回傳 CORS 回應標頭（response header）的 API 發送跨來源 `GET` 請求。

![簡單請求示範](https://raw.githubusercontent.com/QzCurious/seamless-cors/HEAD/demo/cors-simple-requests/cors-simple-requests.gif)

### 預檢請求

瀏覽器送出 JSON `POST` 請求前，會先發送一個 `OPTIONS` 預檢請求（preflight request）。

![預檢請求示範](https://raw.githubusercontent.com/QzCurious/seamless-cors/HEAD/demo/cors-preflighted-requests/cors-preflighted-requests.gif)

## 安裝

### npm

將指令安裝為全域套件：

```sh
npm install --global seamless-cors
```

### Go install

若已安裝 Go 1.27.0 或更新版本，可建置並安裝最新發行版：

```sh
go install github.com/QzCurious/seamless-cors/cmd/seamless-cors@latest
```

請確認 Go 的執行檔目錄（`$(go env GOPATH)/bin`）已加入 `PATH`。

## 功能

### 選擇性處理 CORS

只有符合 Upstream List 設定的請求，才會交給本機代理服務處理；其他流量不受影響。

遇到 CORS 預檢請求，也就是同時帶有 `Origin` 與 `Access-Control-Request-Method` header 的 `OPTIONS` 請求時，seamless-cors 會直接回傳 `204 No Content`，不會把請求送到 API 伺服器。回應會：

- 依照請求設定允許的來源、方法與 header；
- 允許夾帶認證資訊；
- 瀏覽器提出要求時，允許存取私有網路；
- 將 `Access-Control-Max-Age` 設為 `0`，避免瀏覽器快取預檢結果。

實際請求若帶有 `Origin`，仍會照常送到 API 伺服器處理。收到回應後，seamless-cors 會以開發／QA 用的 CORS 設定取代原有的 `Access-Control-*` header、填入請求來源、允許夾帶認證資訊，並開放瀏覽器讀取一般 response header。對於未帶 `Origin` 的請求，API 回應不會被修改；由 seamless-cors 自己產生的錯誤回應也會維持原樣。

這項功能只會調整瀏覽器的 CORS 限制，不會繞過 API 的身分驗證、權限控管、cookie、CSRF 防護，或改變應用程式本身的行為。

HTTP selector 不需要憑證，加入清單後就會生效。HTTPS selector 則必須等開發用 CA（UserCA）安裝完成並可正常使用後才會啟用。在這之前，HTTP 仍可正常運作，`start` 或 `status` 也會顯示 HTTPS 尚未啟用，並提示你執行 `seamless-cors install`。

### HTTPS Facade

HTTPS Facade 可以替只提供 HTTP 的 API 建立一個給瀏覽器使用的 HTTPS 網址，例如：

```text
http://app.test:3000  ->  https://app.test:3000  ->  http://app.test:3000
http://app.test       ->  https://app.test       ->  http://app.test
```

左側是加入清單的 HTTP URL，瀏覽器改用中間的 HTTPS URL，本機代理服務再把請求轉送到右側原本的 HTTP URL。HTTP port 80 會對應到瀏覽器端的 HTTPS port 443，其他 port 則維持不變。只有明確寫出 `http://` 的 Origin Selector 會建立 HTTPS Facade；單純指定 hostname 的 Host Selector 不會。只要 UserCA 可用，HTTPS Facade 就會啟用。

多條規則同時符合時，明確指定的 HTTPS Origin Selector 優先於 HTTPS Facade；完全符合的 HTTPS Facade 也優先於範圍較廣的 Host Selector。規則來自哪個檔案、寫在第幾行，都不會影響優先順序。

如果 API 回傳的重新導向使用完整 URL，而且指回原本的 HTTP origin，seamless-cors 會將它改寫為瀏覽器正在使用的 HTTPS origin。相對 URL，以及導向其他 origin 的 URL，則維持原樣。HTTPS Facade 不會改寫 response body、cookie 或安全性 header，也不會加入 `Forwarded` 或 `X-Forwarded-Proto`，因此原本的 HTTP API 不需要為 HTTPS Facade 做任何調整。

### `upstreams.txt` 的位置與更新方式

seamless-cors 會讀取以下兩個檔案，並將內容合併成實際生效的 Upstream List：

| 來源 | 位置 | 行為 |
| ---- | ---- | ---- |
| Global | 平台設定目錄下的 `seamless-cors/upstreams.txt` | 固定讀取；若檔案不存在，`start` 會詢問是否建立。 |
| Directory | 執行 `start` 時所在目錄中的 `upstreams.txt` | 選用；適合放置只和特定專案或工作目錄有關的設定。 |

Global Upstream List 的預設路徑如下：

| 平台 | 預設路徑 |
| ---- | -------- |
| Linux | `~/.config/seamless-cors/upstreams.txt` |
| macOS | `~/Library/Application Support/seamless-cors/upstreams.txt` |
| Windows | `%LOCALAPPDATA%\seamless-cors\upstreams.txt` |

設定 `XDG_CONFIG_HOME` 後，會改用該目錄。舊版使用的 `~/.seamless-cors/upstreams.txt` 不會被讀取，也不會自動搬移。

兩個檔案會分開監看，合併時不分優先順序。正規化後相同的 selector 只會生效一次。新增、修改、刪除或重新建立任一檔案時，設定都會自動更新，不必重新啟動。Directory Upstream List 的位置會在啟動時決定；若要改用其他目錄中的清單，請先停止 seamless-cors，再從該目錄重新執行 `start`。

空白行與以 `#` 開頭的註解都會略過。也可以在 selector 後面加上空白，再接 `#` 註解。格式有誤的行會被忽略並顯示警告，其他有效設定仍會照常生效。

### Selector 規則

每一個不是空白或註解的行，都必須是 Host Selector 或 Origin Selector：

| 格式 | 範例 | 比對方式 |
| ---- | ---- | -------- |
| 精確 Host Selector | `api.example.com` | 比對該 hostname 上所有 port 的 HTTP；UserCA 可用時，也會比對所有 port 的 HTTPS。 |
| 萬用字元 Host Selector | `*.test.example.com` | 只比對多一層的子網域，例如 `api.test.example.com`；不包含 `test.example.com` 本身，也不包含層級更深的名稱。port 與 protocol 的規則和精確 Host Selector 相同。 |
| HTTP Origin Selector | `http://localhost:3000` | 只比對這個 HTTP origin；UserCA 可用時，也會建立完全對應的 HTTPS Facade。 |
| HTTPS Origin Selector | `https://api.example.com:8443` | 只比對該 HTTPS origin；必須有可用的 UserCA。 |

Host Selector 不能包含 protocol 或 port。Origin Selector 必須以 `http://` 或 `https://` 開頭，不支援萬用字元，也不能包含 path、query string 或 fragment。IPv4 與加上方括號的 IPv6 格式都可以使用。hostname 與 protocol 會轉成小寫；省略 port 時，HTTP 會視為 80，HTTPS 會視為 443，因此 `https://api.example.com` 和 `https://api.example.com:443` 代表同一個設定。

### 指令

下列指令可透過全域安裝的版本執行，也可以加上 `npx seamless-cors ...` 前綴使用。

| 指令 | 行為 |
| ---- | ---- |
| `seamless-cors start` | 在前景啟動服務、監看兩份 Upstream List，並向目前適用的所有 Network Service delivery PAC。若服務已在執行，再次執行 `start` 會保留原執行環境並再次嘗試 delivery。 |
| `seamless-cors stop` | 停止執行中的服務；即使找不到存活的 owner，也會盡力清理 seamless-cors 所屬的 System PAC 與暫存狀態。若清理結果不確定，命令仍會成功完成並醒目回報可能的殘留。 |
| `seamless-cors status` | 重新觀察所有可見的 Network Service，顯示服務、代理規則、CORS、HTTPS Facade、Upstream List、System PAC 與 UserCA 的目前狀態，不會變更任何設定。保留的 delivery 診斷會標示為歷史資訊。輸出格式以方便閱讀為主，不保證能作為固定的腳本介面。 |
| `seamless-cors install` | 安裝、修復或更新目前使用者的開發用 CA。若服務正在執行，會立即套用可用的 CA。 |
| `seamless-cors uninstall` | 移除所有 seamless-cors UserCA 與本機 CA 資料。若 HTTPS 攔截正在運作，會先要求確認；確認後會停用 HTTPS，但 HTTP 代理仍可繼續使用。 |
| `seamless-cors serve` | 只啟動本機控制服務，不會開始處理瀏覽器流量，也不會變更 PAC 設定；必須另外執行 `start` 才會啟用代理功能。 |
| `seamless-cors version` | 顯示已安裝的版本。 |

即使 CA 已經安裝，再次執行 `install` 也不會有問題。服務會保持執行，只會在更新 CA 的過程中短暫停用 HTTPS，並在新的 CA 可用後自動恢復。

## 常見問題

### 為什麼 Chrome 無法在 loopback 位址上使用 HTTPS Facade？

HTTPS Facade 可以用在非 loopback 的 origin。例如，UserCA 可用時，將 `http://httpforever.com` 加入 Upstream List，就能透過 `https://httpforever.com` 存取原本的 HTTP 網站。

Chrome 預設不會透過代理伺服器連線到 `localhost`、`*.localhost`、`127.0.0.0/8` 與 `[::1]` 等 loopback 位址。這項規則也適用於 PAC 檔，而且無法由 PAC 本身關閉，因此 seamless-cors 收不到這些請求。舉例來說，即使加入 `http://localhost:3000`，Chrome 開啟 `https://localhost:3000` 時仍不會經過 HTTPS Facade。這是 Chrome 的代理規則限制，不是 HTTPS Facade 本身的限制。

請改用 `app.test` 之類的非 loopback hostname，再透過 hosts 檔案或本機 DNS 將它指向 `127.0.0.1`。接著加入 `http://app.test:3000`，並在瀏覽器開啟 `https://app.test:3000`。Chrome 會先判斷是否使用代理伺服器，再解析 hostname，因此 seamless-cors 可以收到請求並轉送到本機的 HTTP 服務。

手動設定代理伺服器時，可以用 Chrome 特殊的 `<-loopback>` bypass list 規則覆寫這項預設行為；但 PAC 檔仍無法藉此代理 loopback 流量。詳情請參閱 Chromium 的 [Implicit proxy bypass rules](https://chromium.googlesource.com/chromium/src/+/master/net/docs/proxy.md#Implicit-bypass-rules)。

## 儲存位置

| 用途 | 位置 |
| ---- | ---- |
| Global Upstream List | XDG 設定目錄 |
| 已安裝的 UserCA 憑證與金鑰 | XDG state 目錄 |
| 程序鎖定與服務探索資料 | XDG runtime 目錄 |
| Directory Upstream List | 執行 `start` 時所在的目錄 |

若要從使用 `~/.seamless-cors/runtime` 的舊版本升級，請先停止舊版服務。新版不會讀取該目錄，也不會搬移其中的 lock 或 discovery 檔案；舊程序停止後，可手動移除殘留檔案。請勿同時執行新舊版本，因為兩者使用不同的 lock 路徑，可能無法正確避免重複啟動。

## 支援平台

目前支援在 macOS 與 Windows 上自動設定系統代理與使用者憑證。

## 授權條款

[MIT](LICENSE)
