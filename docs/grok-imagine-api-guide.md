# Grok Imagine 绘图 / 视频 API 使用指南

> 本文档说明如何通过本站 API 网关调用 xAI 的 Grok Imagine 系列模型(文生图、图生图、文生视频)。
> 所有接口均兼容 OpenAI 格式,可直接用 curl / OpenAI SDK / 任意 HTTP 客户端调用。

---

## 0. 通用说明

| 项 | 值 |
|---|---|
| **API 地址(Base URL)** | `https://matrix.000328.xyz` |
| **鉴权方式** | 请求头 `Authorization: Bearer <你的令牌>` |
| **令牌格式** | `sk-xxxxxxxx`(在用户后台获取 / 兑换码兑换得到) |
| **计费单位** | 额度(quota),$1 = 500,000 额度 |

三个模型一览:

| 模型名 | 类型 | 接口 | 同步/异步 | 价格 |
|---|---|---|---|---|
| `grok-imagine-image` | 标准画质图片 | `/v1/images/generations` | 同步 | $0.02 / 张 |
| `grok-imagine-image-quality` | 高画质图片 | `/v1/images/generations` | 同步 | $0.05 / 张 |
| `grok-imagine-video` | 视频生成 | `/v1/video/generations` | **异步**(需轮询) | $0.07 / 秒 |

> 注:实际扣费 = 价格 × 数量(或秒数)× 你所在分组的折扣比率。具体折扣以后台「分组倍率」为准。

---

## 1. 图片生成(标准画质)`grok-imagine-image`

最便宜的图片模型,适合批量出图。**同步接口**,请求后直接返回图片地址。

### 接口

```
POST /v1/images/generations
```

### 请求示例

```bash
curl -X POST https://matrix.000328.xyz/v1/images/generations \
  -H "Authorization: Bearer sk-你的令牌" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-image",
    "prompt": "A cute orange cat sitting on a windowsill, soft morning light",
    "n": 1,
    "response_format": "url"
  }'
```

### 参数说明

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `model` | string | 是 | — | 固定填 `grok-imagine-image` |
| `prompt` | string | 是 | — | 图片描述(英文效果最佳)。越具体越好,可写画面、光线、风格、镜头等 |
| `n` | int | 否 | 1 | 一次生成几张。**每张单独计费**(n=4 即扣 4 张的钱) |
| `response_format` | string | 否 | `url` | 返回格式:`url`(返回图片链接,推荐)或 `b64_json`(返回 base64 编码,适合直接嵌入) |

> 说明:本模型**不支持** `size` / `quality` 参数(xAI 标准图模型固定输出尺寸),填了也会被忽略。

### 返回示例

```json
{
  "created": 1781854966,
  "data": [
    { "url": "https://imgen.x.ai/xai-imgen/xai-tmp-imgen-xxxx.jpeg" }
  ],
  "usage": { "cost_in_usd_ticks": 200000000 }
}
```

- `data[].url`:生成的图片直链(注意:为临时链接,请及时下载保存)
- `data[].b64_json`:当 `response_format=b64_json` 时返回 base64 字符串

---

## 2. 图片生成(高画质)`grok-imagine-image-quality`

更高质量的图片模型,用法与标准图**完全一致**,只是把 `model` 换成 `grok-imagine-image-quality`,价格更高($0.05/张)。

### 请求示例

```bash
curl -X POST https://matrix.000328.xyz/v1/images/generations \
  -H "Authorization: Bearer sk-你的令牌" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-image-quality",
    "prompt": "Ultra-detailed portrait of a snow leopard, studio lighting, 8k",
    "n": 1,
    "response_format": "url"
  }'
```

参数、返回结构与第 1 节相同。适合对画质要求高的场景(海报、封面、产品图等)。

---

## 3. 视频生成 `grok-imagine-video`

文生视频模型。**异步接口**:先提交任务拿到 `task_id`,再轮询查询,任务完成后返回视频下载链接。

### 3.1 提交生成任务

```
POST /v1/video/generations
```

```bash
curl -X POST https://matrix.000328.xyz/v1/video/generations \
  -H "Authorization: Bearer sk-你的令牌" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-video",
    "prompt": "Ocean waves crashing on rocks at sunset, cinematic slow motion",
    "seconds": "4"
  }'
```

#### 参数说明

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `model` | string | 是 | — | 固定填 `grok-imagine-video` |
| `prompt` | string | 是 | — | 视频描述(英文最佳)。可写动作、镜头、运镜、光线、风格 |
| `seconds` | string | 否 | `"4"` | 视频时长(秒),范围 **1–15**。**按秒计费**,越长越贵。注意是字符串,如 `"10"` |
| `size` | string | 否 | `"720x1280"` | 分辨率。默认竖屏 720×1280;可填 `"1280x720"`(横屏)等 |

> 计费 = $0.07 × 秒数 × 分组折扣。例:10 秒视频 = $0.07 × 10 = $0.70(再乘分组折扣)。

#### 提交后返回

```json
{
  "task_id": "task_mrviW8iu7Y6ij9TKaJQoX5KWCCcgpomb",
  "id": "task_mrviW8iu7Y6ij9TKaJQoX5KWCCcgpomb",
  "object": "video",
  "model": "grok-imagine-video",
  "status": "queued",
  "progress": 0,
  "seconds": "4",
  "size": "720x1280",
  "created_at": 1781857021
}
```

记下返回的 **`task_id`**,下一步轮询要用。

### 3.2 轮询查询任务状态

```
GET /v1/video/generations/{task_id}
```

```bash
curl https://matrix.000328.xyz/v1/video/generations/task_mrviW8iu7Y6ij9TKaJQoX5KWCCcgpomb \
  -H "Authorization: Bearer sk-你的令牌"
```

#### 状态字段(`data.status`)

| 状态 | 含义 | 处理 |
|---|---|---|
| `QUEUED` | 排队中 | 继续轮询 |
| `IN_PROGRESS` | 生成中 | 继续轮询 |
| `SUCCESS` | ✅ 完成 | 从 `data.result_url` 取视频链接 |
| `FAILURE` | ❌ 失败 | 看 `data.fail_reason`,失败会自动退款 |

建议每 **5–10 秒**轮询一次,一个 4 秒视频通常 **30–60 秒**完成。

#### 完成后返回

```json
{
  "code": "success",
  "data": {
    "task_id": "task_mrviW8iu...",
    "status": "SUCCESS",
    "progress": "100%",
    "result_url": "https://vidgen.x.ai/xai-vidgen-bucket/xai-video-xxxx.mp4",
    "data": {
      "status": "done",
      "progress": 100,
      "video": {
        "url": "https://vidgen.x.ai/xai-vidgen-bucket/xai-video-xxxx.mp4",
        "duration": 4
      }
    }
  }
}
```

- **`data.result_url`**:视频下载直链(MP4)。为临时链接,请尽快下载保存。

### 3.3 完整流程示例(bash)

```bash
TOKEN="sk-你的令牌"
BASE="https://matrix.000328.xyz"

# 1. 提交
TASK_ID=$(curl -s -X POST "$BASE/v1/video/generations" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"model":"grok-imagine-video","prompt":"a dog running on the beach","seconds":"4"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['task_id'])")

echo "任务已提交: $TASK_ID"

# 2. 轮询直到完成
while true; do
  sleep 8
  RESP=$(curl -s "$BASE/v1/video/generations/$TASK_ID" -H "Authorization: Bearer $TOKEN")
  STATUS=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['status'])")
  echo "状态: $STATUS"
  if [ "$STATUS" = "SUCCESS" ]; then
    URL=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['result_url'])")
    echo "视频地址: $URL"
    curl -fsSL -o output.mp4 "$URL"   # 3. 下载
    echo "已保存到 output.mp4"
    break
  elif [ "$STATUS" = "FAILURE" ]; then
    echo "生成失败"; break
  fi
done
```

---

## 4. 常见问题

**Q:视频链接打不开 / 过期了?**
xAI 返回的图片、视频链接都是**临时链接**(几小时内有效),请生成后立即用 `curl`/浏览器下载保存到本地。

**Q:提示 `insufficient_quota` / 额度不足?**
余额不够,请用兑换码充值后再试。

**Q:视频一直 QUEUED 不动?**
高峰期上游排队较慢,耐心轮询;超过 5 分钟仍无结果可重新提交。

**Q:中文 prompt 行不行?**
能识别,但**英文 prompt 效果明显更好**,建议用英文描述。

**Q:怎么算一次要花多少钱?**
- 图片:`张数 × 单价`(标准 $0.02,高画质 $0.05)
- 视频:`秒数 × $0.07`
- 最终再乘以你所在分组的折扣倍率(以后台为准)。

---

*最后更新:2026-06-19*
