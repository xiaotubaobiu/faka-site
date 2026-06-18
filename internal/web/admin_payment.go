package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"faka-site/internal/epay"
	"faka-site/internal/payment"
)

// maxKeyUploadSize caps how much we'll accept for a single key-file upload
// (PEM keys are ~1.6KB; APIv3 keys are 32 bytes). Plenty of headroom.
const maxKeyUploadSize = 8 * 1024

// ---------- Key file upload / delete ----------

// postKeyUpload receives a single PEM/key file and writes it into the key dir.
// Form fields: name (the target filename, must be one of the known specs),
// file (the uploaded content).
func (s *Server) postKeyUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxKeyUploadSize); err != nil {
		s.renderKeyManage(w, r, "解析上传失败:"+err.Error())
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if !isKnownKeyFile(name) {
		s.renderKeyManage(w, r, "未知的密钥文件名:"+name)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		s.renderKeyManage(w, r, "请选择要上传的文件")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxKeyUploadSize))
	if err != nil {
		s.renderKeyManage(w, r, "读取文件失败:"+err.Error())
		return
	}
	if len(content) == 0 {
		s.renderKeyManage(w, r, "文件内容为空")
		return
	}
	if err := payment.WriteKeyFile(name, string(content)); err != nil {
		log.Printf("key upload: write %s failed: %v", name, err)
		s.renderKeyManage(w, r, "写入失败:"+err.Error())
		return
	}
	log.Printf("key upload: wrote %s (%d bytes)", name, len(content))
	s.renderKeyManage(w, r, "已上传 "+name)
}

// postKeyDelete removes a key file (sets that channel back to unconfigured).
func (s *Server) postKeyDelete(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	if !isKnownKeyFile(name) {
		s.renderKeyManage(w, r, "未知的密钥文件名:"+name)
		return
	}
	if err := payment.DeleteKeyFile(name); err != nil {
		s.renderKeyManage(w, r, "删除失败:"+err.Error())
		return
	}
	log.Printf("key delete: removed %s", name)
	s.renderKeyManage(w, r, "已删除 "+name)
}

func isKnownKeyFile(name string) bool {
	for _, spec := range payment.KeyFileSpecs {
		if spec.Name == name {
			return true
		}
	}
	return false
}

// ---------- EPay merchant CRUD ----------

// epayMerchantView is a merchant row for the management UI.
type epayMerchantView struct {
	PID int    `json:"pid"`
	Key string `json:"key"`
}

// loadMerchants reads the epay_merchants JSON from the config store.
func (s *Server) loadMerchants() []epayMerchantView {
	ctx := context.Background()
	raw, err := s.store.GetConfig(ctx, "epay_merchants")
	if err != nil || raw == "" {
		return nil
	}
	var ms []epayMerchantView
	if err := json.Unmarshal([]byte(raw), &ms); err != nil {
		return nil
	}
	return ms
}

// saveMerchants writes the merchant list back to the config store as JSON.
func (s *Server) saveMerchants(ms []epayMerchantView) error {
	ctx := context.Background()
	if len(ms) == 0 {
		return s.store.SetConfig(ctx, "epay_merchants", "")
	}
	out, err := json.Marshal(ms)
	if err != nil {
		return err
	}
	return s.store.SetConfig(ctx, "epay_merchants", string(out))
}

// getMerchants renders the epay merchant management page.
func (s *Server) getMerchants(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "admin_merchants.html", ViewData{
		Title: "商户管理",
		Data:  map[string]any{"merchants": s.loadMerchants()},
	})
}

// postMerchantAdd adds a new merchant (pid + auto-generated key, or custom key).
func (s *Server) postMerchantAdd(w http.ResponseWriter, r *http.Request) {
	pidStr := strings.TrimSpace(r.PostFormValue("pid"))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		s.renderMerchants(w, r, "PID 必须是正整数")
		return
	}
	key := strings.TrimSpace(r.PostFormValue("key"))
	if key == "" {
		key = genMerchantKey()
	}
	ms := s.loadMerchants()
	for _, m := range ms {
		if m.PID == pid {
			s.renderMerchants(w, r, fmt.Sprintf("PID %d 已存在", pid))
			return
		}
	}
	ms = append(ms, epayMerchantView{PID: pid, Key: key})
	if err := s.saveMerchants(ms); err != nil {
		s.renderMerchants(w, r, "保存失败:"+err.Error())
		return
	}
	s.renderMerchants(w, r, "已添加商户 "+strconv.Itoa(pid))
}

// postMerchantDelete removes a merchant by pid.
func (s *Server) postMerchantDelete(w http.ResponseWriter, r *http.Request) {
	pid, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("pid")))
	if pid <= 0 {
		s.renderMerchants(w, r, "无效的 PID")
		return
	}
	ms := s.loadMerchants()
	out := ms[:0]
	for _, m := range ms {
		if m.PID != pid {
			out = append(out, m)
		}
	}
	if err := s.saveMerchants(out); err != nil {
		s.renderMerchants(w, r, "保存失败:"+err.Error())
		return
	}
	s.renderMerchants(w, r, "已删除商户 "+strconv.Itoa(pid))
}

// postMerchantResetKey rotates a merchant's communication key.
func (s *Server) postMerchantResetKey(w http.ResponseWriter, r *http.Request) {
	pid, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("pid")))
	if pid <= 0 {
		s.renderMerchants(w, r, "无效的 PID")
		return
	}
	newKey := genMerchantKey()
	ms := s.loadMerchants()
	changed := false
	for i := range ms {
		if ms[i].PID == pid {
			ms[i].Key = newKey
			changed = true
			break
		}
	}
	if !changed {
		s.renderMerchants(w, r, fmt.Sprintf("PID %d 不存在", pid))
		return
	}
	if err := s.saveMerchants(ms); err != nil {
		s.renderMerchants(w, r, "保存失败:"+err.Error())
		return
	}
	s.renderMerchants(w, r, "已重置商户 "+strconv.Itoa(pid)+" 的密钥")
}

func (s *Server) renderMerchants(w http.ResponseWriter, r *http.Request, msg string) {
	s.render(w, r, "admin_merchants.html", ViewData{
		Title: "商户管理",
		Data: map[string]any{
			"merchants": s.loadMerchants(),
			"msg":       msg,
		},
	})
}

// renderKeyManage re-renders the config page (which hosts the key section)
// with a status message.
func (s *Server) renderKeyManage(w http.ResponseWriter, r *http.Request, msg string) {
	cfg, _ := s.config()
	s.render(w, r, "admin_config.html", ViewData{
		Title: "配置",
		Data: map[string]any{
			"cfg":    cfg,
			"keys":   payment.KeyFileStatuses(),
			"keyMsg": msg,
		},
	})
}

// genMerchantKey returns a 32-char hex random string for a merchant comm key.
func genMerchantKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- Admin docs page ----------

func (s *Server) getAdminDocs(w http.ResponseWriter, r *http.Request) {
	// Pass through merchant base info so the doc page can show the gateway URL.
	cfg := s.mustConfig()
	ec := s.epayConfig()
	primary := epay.Merchant{}
	if ms := ec.Merchants; len(ms) > 0 {
		primary = ms[0]
	}
	s.render(w, r, "admin_docs.html", ViewData{
		Title: "支付配置文档",
		Data: map[string]any{
			"primaryMerchant": primary,
			"notifyBase":      cfg.RechargeNotifyBase,
			"keys":            payment.KeyFileStatuses(),
		},
	})
}
