include $(TOPDIR)/rules.mk

PKG_NAME:=mcastferry
PKG_VERSION:=0.1.0
PKG_RELEASE:=1

PKG_SOURCE:=$(PKG_NAME)-$(PKG_VERSION).tar.gz
PKG_SOURCE_VERSION:=0ce01f57513b75582b5044f7c82a90ddf314436f
PKG_SOURCE_URL:=https://codeload.github.com/zesuy/mcastferry/tar.gz/$(PKG_SOURCE_VERSION)?
PKG_HASH:=21ed8d80ec5f50264b44f7581ef0811beeed391890675a349f44a5adf6d55f3b
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_SOURCE_VERSION)

PKG_MAINTAINER:=Zesuy <hongri580@163.com>
PKG_LICENSE:=MIT
PKG_LICENSE_FILES:=LICENSE
PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

GO_PKG:=github.com/zesuy/mcastferry
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/mcastferry
GO_PKG_LDFLAGS_X:=$(GO_PKG)/cmd/mcastferry.version=$(PKG_VERSION)

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/mcastferry
  SECTION:=net
  CATEGORY:=Network
  TITLE:=IPv4 multicast to HTTP relay
  URL:=https://github.com/zesuy/mcastferry
  DEPENDS:=$(GO_ARCH_DEPENDS) +rpcd +rpcd-mod-ucode
endef

define Package/mcastferry/description
 McastFerry provides a bounded IPv4 ASM multicast to close-delimited HTTP relay.
endef

define Package/mcastferry/conffiles
/etc/config/mcastferry
endef

define Package/mcastferry/install
	$(call GoPackage/Package/Install/Bin,$(PKG_INSTALL_DIR))
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) $(PKG_INSTALL_DIR)/usr/bin/mcastferry $(1)/usr/bin/
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/etc/config/mcastferry $(1)/etc/config/mcastferry
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/etc/init.d/mcastferry $(1)/etc/init.d/mcastferry
	$(INSTALL_DIR) $(1)/usr/libexec
	$(INSTALL_BIN) ./files/usr/libexec/mcastferry-install-playlist $(1)/usr/libexec/
	$(INSTALL_DIR) $(1)/usr/share/rpcd/ucode
	$(INSTALL_DATA) ./files/usr/share/rpcd/ucode/mcastferry.uc $(1)/usr/share/rpcd/ucode/
endef

define Package/luci-app-mcastferry
  SECTION:=luci
  CATEGORY:=LuCI
  SUBMENU:=3. Applications
  TITLE:=LuCI support for McastFerry
  PKGARCH:=all
  DEPENDS:=+mcastferry +luci-base
endef

define Package/luci-app-mcastferry/description
 Native JavaScript LuCI configuration and playlist upload for McastFerry.
endef

define Package/luci-app-mcastferry/install
	$(INSTALL_DIR) $(1)/usr/share/rpcd/acl.d
	$(INSTALL_DATA) ./files/usr/share/rpcd/acl.d/luci-app-mcastferry.json $(1)/usr/share/rpcd/acl.d/
	$(INSTALL_DIR) $(1)/usr/share/luci/menu.d
	$(INSTALL_DATA) ./files/usr/share/luci/menu.d/luci-app-mcastferry.json $(1)/usr/share/luci/menu.d/
	$(INSTALL_DIR) $(1)/www/luci-static/resources/view/mcastferry
	$(INSTALL_DATA) ./files/www/luci-static/resources/view/mcastferry/settings.js $(1)/www/luci-static/resources/view/mcastferry/
endef

$(eval $(call GoBinPackage,mcastferry))
$(eval $(call BuildPackage,mcastferry))
$(eval $(call BuildPackage,luci-app-mcastferry))
