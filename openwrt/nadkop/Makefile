include $(TOPDIR)/rules.mk

PKG_NAME:=nadkop
PKG_VERSION:=0.3.0
PKG_RELEASE:=6
PKG_LICENSE:=AGPL-3.0-or-later
PKG_MAINTAINER:=REALalphas
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)
PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16
PKG_FILE_DEPENDS:=$(CURDIR)/../../go.mod $(CURDIR)/../../cmd $(CURDIR)/../../internal $(CURDIR)/../../examples

GO_PKG:=nadkop
GO_PKG_BUILD_PKG:=nadkop/cmd/nadkop

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/nadkop
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Xray and zapret routing manager
  DEPENDS:=$(GO_ARCH_DEPENDS) +xray-core +nadkop-nfqws +firewall4 +nftables-json +kmod-nft-queue +kmod-nft-tproxy +ip-full +jsonfilter +ca-bundle +libubox +rpcd
endef

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) $(CURDIR)/../../go.mod $(CURDIR)/../../cmd $(CURDIR)/../../internal $(PKG_BUILD_DIR)/
endef

define Package/nadkop/conffiles
/etc/nadkop/config.json
/etc/nadkop/nodes.json
/etc/nadkop/hwid
/etc/nadkop/servers.json
endef

define Package/nadkop/install
	$(call GoPackage/Package/Install/Bin,$(1))
	$(INSTALL_DIR) $(1)/etc/init.d $(1)/etc/nadkop $(1)/usr/libexec/rpcd $(1)/lib/upgrade/keep.d
	$(INSTALL_DATA) ./files/nadkop.keep $(1)/lib/upgrade/keep.d/nadkop
	$(INSTALL_BIN) ./files/usr/libexec/rpcd/nadkop $(1)/usr/libexec/rpcd/nadkop
	$(INSTALL_BIN) ./files/nadkop.init $(1)/etc/init.d/nadkop
	$(INSTALL_CONF) $(CURDIR)/../../examples/openwrt-config.json $(1)/etc/nadkop/config.json
	printf '[]\n' > $(1)/etc/nadkop/nodes.json
	chmod 600 $(1)/etc/nadkop/nodes.json
	$(INSTALL_DIR) $(1)/usr/share/nftables.d/chain-pre/input $(1)/usr/share/nftables.d/ruleset-post
	$(INSTALL_DATA) ./files/usr/share/nftables.d/chain-pre/input/90-nadkop.nft $(1)/usr/share/nftables.d/chain-pre/input/90-nadkop.nft
	$(INSTALL_DATA) ./files/usr/share/nftables.d/ruleset-post/90-nadkop.nft $(1)/usr/share/nftables.d/ruleset-post/90-nadkop.nft
	# An empty conffile is seeded on-device; no shared HWID is baked into packages.
	# Subscription URLs and HWID are world-readable until install-openwrt.sh
	# runs, so lock them down here too for plain `apk add` / `opkg install`.
	$(INSTALL_CONF) ./files/hwid $(1)/etc/nadkop/hwid
	$(INSTALL_CONF) ./files/servers.json $(1)/etc/nadkop/servers.json
	chmod 600 $(1)/etc/nadkop/config.json $(1)/etc/nadkop/hwid $(1)/etc/nadkop/servers.json
endef

define Package/nadkop/postinst
#!/bin/sh
# Pick up the new `nadkop rpc` binary without a reboot when only the core
# package was upgraded (full installer does this itself for 3-package sets).
[ -n "$${IPKG_INSTROOT}" ] || /etc/init.d/rpcd restart >/dev/null 2>&1 || true
exit 0
endef

$(eval $(call GoBinPackage,nadkop))
$(eval $(call BuildPackage,nadkop))
