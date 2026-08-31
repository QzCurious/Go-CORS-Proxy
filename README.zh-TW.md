# seamless-cors

[English](README.md) | 繁體中文

`seamless-cors` 讓瀏覽器能直接呼叫指定的開發與 QA 上游服務，就像這些服務原本就採用寬鬆的 CORS 設定。應用程式使用的 URL 不必變更：由工具管理的 Proxy Auto-Configuration（PAC）檔只會將你選取的上游服務導向本機 gateway，其餘流量仍會直接連線。

HTTP 不需設定憑證即可使用。原生 HTTPS 與 HTTPS Facade 則會使用 seamless-cors 的開發用 CA，且必須由你明確執行安裝。

> [!NOTE]
> 此專案仍在積極開發中。

> [!WARNING]
> seamless-cors 僅供本機開發與 QA 環境使用。

## 快速開始

直接透過 `npx` 啟動 gateway：

```sh
npx seamless-cors start
```

第一次執行時，請依提示建立 Global Upstream List，接著在指令顯示的路徑中逐行加入上游服務：

```text
api.dev.example.com
http://localhost:3000
https://api.example.com:8443
```

儲存檔案後，再試一次瀏覽器請求。gateway 執行期間會自動套用變更，不需要重新啟動。

`start` 會持續在目前的終端機前景執行，並暫時為適用的系統網路服務設定 PAC URL。若原本已有不屬於 seamless-cors 的 PAC 設定，seamless-cors 不會動到它。使用完畢後按下 `Ctrl+C`，即可停止 gateway，並移除由 seamless-cors 設定的 PAC。

若要使用原生 HTTPS 或 HTTPS Facade，請安裝開發用 CA，並允許作業系統顯示的授權提示：

```sh
npx seamless-cors install
```

若 gateway 已在執行，符合條件的 HTTPS 路由會立即啟用。gateway 停止後，CA 仍會保留在系統中；若要移除，請明確執行 `npx seamless-cors uninstall`。

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

只有符合 Upstream List 的請求才會經由 gateway 轉送。

遇到 CORS 預檢請求，也就是同時帶有 `Origin` 與 `Access-Control-Request-Method` header 的 `OPTIONS` 請求時，seamless-cors 會直接回傳 `204 No Content`，不會將請求送往上游服務。回應會：

- 依照請求設定允許的來源、方法與 header；
- 允許夾帶憑證資訊；
- 瀏覽器提出要求時，允許存取私人網路；
- 將 `Access-Control-Max-Age` 設為 `0`，避免瀏覽器快取預檢結果。

實際請求若帶有 `Origin`，仍會由上游服務照常處理。回傳途中，seamless-cors 會以開發／QA 用規則取代原有的 `Access-Control-*` header、依照請求設定允許的來源、允許夾帶憑證資訊，並讓瀏覽器能讀取一般回應標頭。未帶 `Origin` 的請求，其回應不會被修改；gateway 自行產生的錯誤回應也不會被修改。

這項功能只會改變瀏覽器的 CORS 限制，不會繞過上游服務的身分驗證、權限控管、cookie、CSRF 防護或應用程式本身的行為。

HTTP selector 會立即生效。HTTPS selector 只有在已安裝的 UserCA 可用時才會啟用；否則 HTTP 仍可正常使用，而 `start` 或 `status` 會將 HTTPS 顯示為受阻，並提示執行 `seamless-cors install`。

### HTTPS Facade

HTTPS Facade 會替指定的 HTTP origin 提供一個給瀏覽器使用的 HTTPS 位址，例如：

```text
http://app.test:3000  ->  https://app.test:3000  ->  http://app.test:3000
http://app.test       ->  https://app.test       ->  http://app.test
```

第一個箭頭代表瀏覽器使用的 URL，第二個箭頭則代表 gateway 實際轉送請求的目的地。HTTP 的 port 80 會對應到瀏覽器端 HTTPS 的 port 443，其他 port 則維持不變。只有 HTTP Origin Selector 會建立 HTTPS Facade，Host Selector 不會；只要 UserCA 可用，HTTPS Facade 就會啟用。

selector 發生重疊時，原生 HTTPS Origin Selector 的優先順序高於 HTTPS Facade；精確符合的 HTTPS Facade 也優先於涵蓋範圍較廣的 Host Selector。selector 來自哪個檔案、位於第幾行，都不會影響這套比對順序。

若絕對重新導向（absolute redirect）的目的地是原本指定的 HTTP origin，seamless-cors 會將它改寫為瀏覽器端使用的 HTTPS origin。相對重新導向與指向其他 origin 的重新導向則維持原樣。HTTPS Facade 不會改寫回應內容、cookie 或安全性 header，也不會加入 `Forwarded` 或 `X-Forwarded-Proto`，因此 HTTP 上游服務不需要為 HTTPS Facade 提供額外支援。

### `upstreams.txt` 的位置與更新方式

以下兩個檔案會合併為同一份 Effective Upstream List：

| 來源 | 位置 | 行為 |
| ---- | ---- | ---- |
| Global | 平台設定目錄下的 `seamless-cors/upstreams.txt` | 一律監看；若檔案不存在，`start` 會詢問是否建立。 |
| Directory | 啟動 gateway 時所在目錄中的 `upstreams.txt` | 選用；適合放置只和特定工作目錄有關的設定。 |

Global Upstream List 的預設路徑如下：

| 平台 | 預設路徑 |
| ---- | -------- |
| Linux | `~/.config/seamless-cors/upstreams.txt` |
| macOS | `~/Library/Application Support/seamless-cors/upstreams.txt` |
| Windows | `%LOCALAPPDATA%\seamless-cors\upstreams.txt` |

`XDG_CONFIG_HOME` 可覆寫平台設定目錄。舊版使用的 `~/.seamless-cors/upstreams.txt` 不會被讀取，也不會自動搬移。

兩個檔案會分開監看，再以不分來源優先順序的方式合併。正規化後相同的 selector 只會啟用一次。新增、修改、刪除或重新建立任一檔案時，路由都會自動更新，不必重新啟動。Directory Upstream List 的位置在 gateway 執行期間不會改變；若要改用其他目錄中的清單，請先停止 gateway，再從該目錄重新啟動。

空白行與以 `#` 開頭的註解都會略過。selector 後方若先以空白隔開，也可以接續 `#` 註解。格式有誤的行會被忽略並顯示警告，其餘有效設定仍會照常啟用。

### Selector 規則

每一個非註解行都是 Host Selector 或 Origin Selector：

| 格式 | 範例 | 比對方式 |
| ---- | ---- | -------- |
| 精確 Host Selector | `api.example.com` | 比對該 hostname 上任意 port 的 HTTP；UserCA 可用時，也比對任意 port 的 HTTPS。 |
| 萬用字元 Host Selector | `*.test.example.com` | 只比對最前方一層子網域，例如 `api.test.example.com`；不包含上層網域或層級更深的名稱。port 與 scheme 的規則和精確 Host Selector 相同。 |
| HTTP Origin Selector | `http://localhost:3000` | 只比對該 HTTP origin；UserCA 可用時，另提供完全對應的 HTTPS Facade。 |
| HTTPS Origin Selector | `https://api.example.com:8443` | 只比對該 HTTPS origin；必須有可用的 UserCA。 |

Host Selector 不能包含 scheme 或 port。Origin Selector 必須以 `http://` 或 `https://` 開頭，不支援萬用字元，也不能包含 path、query 或 fragment。IPv4 與加上方括號的 IPv6 格式皆可使用。hostname 與 scheme 會正規化為小寫；省略的預設 port 會正規化為 HTTP 的 80 與 HTTPS 的 443，因此 `https://api.example.com` 和 `https://api.example.com:443` 視為相同設定。

### 指令

下列指令可透過全域安裝的版本執行，也可以加上 `npx seamless-cors ...` 前綴使用。

| 指令 | 行為 |
| ---- | ---- |
| `seamless-cors start` | 在前景啟動 gateway、監看兩份 Upstream List，並管理適用網路服務的 PAC 設定。若 gateway 已在執行，再次呼叫 `start` 會回報現有執行狀態。 |
| `seamless-cors stop` | 停止執行中的 gateway；若先前程序留下可觀察到且屬於 seamless-cors 的 PAC 或 runtime 狀態，也會進行清理。 |
| `seamless-cors status` | 在不變更狀態的前提下，回報 gateway、路由、CORS、facade、Upstream List、PAC 與 UserCA 狀態。輸出內容以供人閱讀為主，不是穩定的腳本介面。 |
| `seamless-cors install` | 安裝、修復或更新目前使用者的開發用 CA。若 gateway 正在執行，會立即採用可用的 CA。 |
| `seamless-cors uninstall` | 移除所有 seamless-cors UserCA 與本機 CA 資料。若 HTTPS 攔截正在運作，會先要求確認；確認後會停用 HTTPS，但執行中 gateway 的 HTTP 處理仍可繼續使用。 |
| `seamless-cors serve` | 只啟動本機 gateway 的控制端，不會開始處理瀏覽器流量，也不會變更 PAC 設定；必須另外執行 `start`，才會啟用 runtime。 |
| `seamless-cors version` | 顯示已安裝的版本。 |

CA 已安裝時，再次執行 `install` 也是安全的。gateway 會保持執行，只短暫撤下仰賴 CA 的 HTTPS 路由、更新 CA，並在替代 CA 可用後恢復這些路由。

## 常見問題

### 為什麼 Chrome 無法在回送（loopback）位址上使用 HTTPS Facade？

HTTPS Facade 可用於非回送位址的 origin。例如，在 HTTPS 已就緒時，為 `http://httpforever.com` 設定 Origin Selector，就能透過 `https://httpforever.com` 存取該 origin。

Chrome 會略過代理伺服器，直接連往 `localhost`、`*.localhost`、`127.0.0.0/8` 與 `[::1]` 等回送目的地。這項規則也適用於 PAC 指令碼，且無法由 PAC 檔停用，因此 seamless-cors 根本收不到這類請求。舉例來說，設定 `http://localhost:3000` Origin Selector，無法讓 Chrome 中的 `https://localhost:3000` 經由 HTTPS Facade。這是 Chrome 的路由限制，並非 HTTPS Facade 本身的限制。

請改用 `app.test` 之類的非回送 hostname，再透過 hosts 檔案或本機 DNS 將它解析到 `127.0.0.1`。接著設定 `http://app.test:3000`，並開啟 `https://app.test:3000`。Chrome 會先決定是否使用代理伺服器，再解析該 hostname，因此 seamless-cors 能將請求轉送到本機 origin。

若使用手動設定的代理伺服器，可透過 Chrome 特殊的 `<-loopback>` 略過清單規則覆寫預設行為；但 PAC 指令碼仍無法藉此代理回送流量。詳情請參閱 Chromium 的[隱含代理略過規則（implicit proxy bypass rules）](https://chromium.googlesource.com/chromium/src/+/master/net/docs/proxy.md#Implicit-bypass-rules)。

## 儲存位置

| 用途 | 位置 |
| ---- | ---- |
| Global Upstream List | XDG 設定目錄 |
| 已安裝的 UserCA 資料 | XDG state 目錄 |
| Gateway lock 與 discovery state | XDG runtime 目錄 |
| Directory Upstream List | 啟動時的工作目錄 |

若要從使用 `~/.seamless-cors/runtime` 的舊版本升級，請先停止舊版 gateway。新版不會讀取該目錄，也不會搬移其中的 lock 或 discovery 檔案；舊程序停止後，可手動移除殘留的舊檔。請勿同時執行新舊版本，因為兩者使用不同的 lock 路徑，可能會繞過互斥機制。

## 支援平台

目前具備受管理平台整合的系統為 macOS 與 Windows。

## 授權條款

[MIT](LICENSE)
