package web

import (
	"errors"
	"faka-site/internal/newapi"
	"faka-site/internal/store"
	"testing"
)

func TestFriendlyBuyError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{store.ErrInsufficient, "余额不足,请联系管理员充值"},
		{newapi.ErrCompliance, "服务暂不可用(兑换码功能未开启)"},
		{newapi.ErrUnauthorized, "服务暂不可用(配置异常)"},
		{newapi.ErrUpstream, "生成失败,请稍后重试"},
		{errors.New("boom"), "购买失败,请稍后重试"},
	}
	for _, c := range cases {
		if got := friendlyBuyError(c.err); got != c.want {
			t.Fatalf("for %v: got %q want %q", c.err, got, c.want)
		}
	}
}
