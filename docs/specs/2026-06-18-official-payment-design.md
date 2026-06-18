# 官方支付配置指南

本系统已从「个人收款码 + 短信猜金额」升级为**直连官方支付**(支付宝当面付 + 微信支付 v3 Native),
对外以标准 **epay 协议**暴露接口,供下游商户对接。本文档说明如何配置官方凭证。

## 架构概览

```
下游商户(含 faka-site 自身充值)
   │  标准 epay 协议(pid + MD5 sign)
   ▼
┌────────────── faka-site 内置 epay 网关 ──────────────┐
│  submit.php / mapi.php → 验签 → 建 epay_order         │
│     → payment.Provider.CreatePayment()                │
│         ├ alipay: alipay.trade.precreate → qr_code   │
│         └ wxpay:  /v3/pay/transactions/native → code_url │
│     → 返回真实动态二维码                               │
│                                                       │
│  官方异步回调                                          │
│     /notify/alipay、/notify/wxpay (public)            │
│     → Provider.ParseNotify 验签/解密                  │
│     → EpayUpdatePaid                                  │
│     → notifyDownstream(签名转发下游)                   │
│       → faka-site /recharge/notify → SettleRecharge   │
└───────────────────────────────────────────────────────┘
```

关键点:幂等结算链路(`SettleRecharge`)完全复用,不会重复到账。

---

## 1. 设置加密主密钥(PAY_SECRET)

支付私钥、APIv3 密钥等敏感配置在入库前会用 AES-256-GCM 加密。
主密钥从环境变量 `PAY_SECRET` 读取。

```bash
# 生成 32 字节随机密钥(64 位十六进制)
openssl rand -hex 32
```

写入 `.env`:

```bash
PAY_SECRET=<上面生成的值>
```

> ⚠️ **换机器/重新部署时必须用同一个值**,否则已加密的密钥无法解密还原。
> 没配置 `PAY_SECRET` 时,加密字段会退化为明文存储(不安全),且保存时报错。

---

## 2. 申请官方凭证

### 支付宝(当面付 / 扫码)
1. 登录 [支付宝开放平台](https://open.alipay.com/),创建「网页&移动应用」或开通「当面付」。
2. 获取:
   - **应用 AppID**
   - **应用私钥**(PEM 格式,RSA2048)— 在「应用公钥/私钥」处生成或上传
   - **支付宝公钥**(PEM 格式)— 用于验签回调
3. 配置在后台 `/admin/config` →「支付宝官方」分区。

### 微信支付(v3 Native 扫码)
1. 登录 [微信支付商户平台](https://pay.weixin.qq.com/),开通 Native 支付。
2. 获取:
   - **AppID**(绑定该商户号的公众号/应用 AppID)
   - **商户号 MchID**
   - **商户证书序列号**(mch_serial_no)
   - **APIv3 密钥**(32 位字符串,商户平台设置)
   - **商户私钥**(PEM 格式,RSA2048,从下载的商户证书解出)
3. 配置在后台 `/admin/config` →「微信支付官方」分区。

---

## 3. 配置后台

进入 `/admin/config`,在对应分区填写:

| 分区 | 字段 | 说明 |
|------|------|------|
| **支付宝官方** | AppID、应用私钥、支付宝公钥、网关、沙箱 | 私钥/公钥加密存储,留空=不修改 |
| **微信支付官方** | AppID、MchID、证书序列号、APIv3密钥、商户私钥 | APIv3密钥/私钥加密存储 |
| **epay 对外网关** | 商户(pid,key)、订单超时、管理员账密 | 给下游商户对接用 |
| **充值设置** | 公网回调基地址、汇率、自身 pid | 回调地址**必须 https** |

### 公网回调基地址(关键)

```
公网回调基地址 = https://你的域名
```

系统据此拼接出官方回调地址(填到后台展示的对接信息里,或直接由系统自动用):

- 支付宝回调:`https://你的域名/notify/alipay`
- 微信回调:`https://你的域名/notify/wxpay`

> 微信 v3 强制要求 HTTPS。基地址末尾的 `/` 会被自动去除。

---

## 4. 下游商户对接(对外 epay 接口)

在「epay 对外网关」分区配置商户(每行 `pid,key`),例如:

```
1001,商户A的通信密钥
1002,商户B的通信密钥
```

下游按标准 epay 协议调用:

### 页面支付(返回 HTML 收银台)
```
GET/POST https://你的域名/submit.php
参数: pid, out_trade_no, notify_url, return_url, name, money, type(alipay|wxpay), sign
```

### 接口支付(返回 JSON)
```
POST https://你的域名/mapi.php  (同上参数)
返回: { "code":1, "trade_no":"EP...", "qrcode":"..." }
```

### 订单查询
```
GET https://你的域名/api.php?act=order&pid=..&key=..&trade_no=..
返回: { "code":1, "data":{ "status":1, ... } }  // status 1=已支付
```

### 回调通知(发给下游 notify_url)
支付成功后,网关用 MD5 签名向下游的 `notify_url` 发 GET 请求:

```
pid, trade_no, out_trade_no, type, name, money, trade_status=TRADE_SUCCESS, param, sign, sign_type=MD5
```

下游校验 sign 后回写 `success`(小写)即完成。详见 `internal/epay/sign.go` 的 `Sign`。

---

## 5. faka-site 自身充值

faka-site 自身的 `/recharge` 就是这个网关的「第一个内部商户」(使用「自身充值商户 pid」)。
无需单独配置——填好上面的官方凭证和回调基地址即可,用户充值时会生成真实官方二维码,
支付后自动到账(复用同一套幂等结算)。

## 6. 调试

- 未配置某渠道时,该渠道下单返回明确错误「支付渠道(X)未配置」,不会静默返回空码。
- epay 管理后台:`/epay/admin`(Basic-Auth,用「epay 管理用户名/密码」登录),可看订单和渠道就绪状态。
- 日志关键字:`notify/alipay`、`notify/wxpay`、`recharge/notify`、`downstream notified`。
