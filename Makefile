include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-nvr
PKG_VERSION:=20260122
PKG_RELEASE:=1

LUCI_TITLE:=LuCI Support for Network Video Recorder
LUCI_DESCRIPTION:=A LuCI application for network video recording using IP cameras.
LUCI_DEPENDS:=+luci-base +lsblk +coreutils-stat +luci-compat
LUCI_PKGARCH:=$(ARCH)

# 保存 package 源目录路径。luci.mk 的 Build/Prepare 只复制 luasrc/htdocs/root/src
# 到 PKG_BUILD_DIR，不会复制 nvr-core-bin / nvr.conf / nvr.init 等单文件，
# 因此 install 段必须用 $(NVR_PKG_DIR) 显式引用源目录，而非 ./（指向 PKG_BUILD_DIR）。
NVR_PKG_DIR := $(CURDIR)

include $(TOPDIR)/feeds/luci/luci.mk

define Package/luci-app-nvr/conffiles
/etc/config/nvr
endef

define Package/luci-app-nvr/install
	# 1. 安装 LuCI 核心文件 (MVC)
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/controller
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/model/cbi
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/view
	$(INSTALL_DATA) $(NVR_PKG_DIR)/luasrc/controller/nvr.lua $(1)/usr/lib/lua/luci/controller/
	$(INSTALL_DATA) $(NVR_PKG_DIR)/luasrc/model/cbi/nvr.lua $(1)/usr/lib/lua/luci/model/cbi/
	$(INSTALL_DATA) $(NVR_PKG_DIR)/luasrc/view/nvr_status.htm $(1)/usr/lib/lua/luci/view/

	# 2. 安装 nvr-core 二进制（CI 中由 Go 交叉编译产生，静态链接无外部依赖）
	$(INSTALL_DIR) $(1)/usr/nvr
	$(INSTALL_BIN) $(NVR_PKG_DIR)/nvr-core-bin $(1)/usr/nvr/nvr-core

	# 3. 安装配置文件
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) $(NVR_PKG_DIR)/nvr.conf $(1)/etc/config/nvr

	# 4. 安装启动脚本
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) $(NVR_PKG_DIR)/nvr.init $(1)/etc/init.d/nvr
endef

$(eval $(call BuildPackage,luci-app-nvr))
