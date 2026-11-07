package ehentai

import "errors"

// 哨兵错误供上层用 errors.Is 分类处理与映射文案；同一条错误链里可能同时包裹多层原因。
var (
	// ErrNotGalleryLink 表示输入不是受支持的 E-Hentai/ExHentai 画廊链接。
	ErrNotGalleryLink = errors.New("ehentai: 不是受支持的画廊链接")
	// ErrAuthRequired 表示站点要求登录：通常是缺少有效 Cookie。
	ErrAuthRequired = errors.New("ehentai: 需要登录 Cookie")
	// ErrNotFound 表示画廊、页面或 token 不存在/无效。
	ErrNotFound = errors.New("ehentai: 画廊或页面不存在")
	// ErrForbidden 表示站点拒绝访问（风控、地区限制等）。
	ErrForbidden = errors.New("ehentai: 站点拒绝访问")
	// ErrRateLimited 表示重试后仍被限流。
	ErrRateLimited = errors.New("ehentai: 请求被限流")
	// ErrParse 表示页面结构与预期不符，站点可能已改版。
	ErrParse = errors.New("ehentai: 页面解析失败")
)
