include $(TOPDIR)/rules.mk

PKG_NAME:=nadkop-nfqws
PKG_VERSION:=2026.11.01
PKG_RELEASE:=1
ZAPRET_COMMIT:=c4d0b1990129dce2b28095277173ee0d7702d304
PKG_SOURCE:=zapret-$(ZAPRET_COMMIT).tar.gz
PKG_SOURCE_URL:=https://codeload.github.com/bol-van/zapret/tar.gz/$(ZAPRET_COMMIT)?
PKG_HASH:=5dce1b550bf6fb8a0706e9b913d466c7e25039d7f78649da937eb1bf055547f4
PKG_BUILD_DIR:=$(BUILD_DIR)/zapret-$(ZAPRET_COMMIT)
PKG_BUILD_PARALLEL:=1
PKG_BUILD_DEPENDS:=libcap
# Upstream zapret keeps its own license; this package only pins and builds it.
PKG_LICENSE:=MIT
PKG_LICENSE_FILES:=docs/LICENSE.txt
PKG_MAINTAINER:=REALalphas

include $(INCLUDE_DIR)/package.mk

define Package/nadkop-nfqws
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Pinned zapret nfqws engine for nadkop
  URL:=https://github.com/bol-van/zapret
  DEPENDS:=+libnetfilter-queue +libnfnetlink +libmnl +zlib
endef

define Build/Compile
	+$(MAKE) $(PKG_JOBS) -C $(PKG_BUILD_DIR)/nfq \
		$(TARGET_CONFIGURE_OPTS) \
		CFLAGS="$(TARGET_CFLAGS) -std=gnu99 -ffunction-sections -fdata-sections" \
		LDFLAGS="$(TARGET_LDFLAGS)" nfqws
endef

define Package/nadkop-nfqws/install
	$(INSTALL_DIR) $(1)/usr/sbin $(1)/usr/share/licenses/nadkop-nfqws
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/nfq/nfqws $(1)/usr/sbin/nfqws
	$(INSTALL_DATA) $(PKG_BUILD_DIR)/docs/LICENSE.txt $(1)/usr/share/licenses/nadkop-nfqws/LICENSE
endef

$(eval $(call BuildPackage,nadkop-nfqws))
