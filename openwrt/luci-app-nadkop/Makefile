include $(TOPDIR)/rules.mk

PKG_VERSION:=0.3.0
PKG_RELEASE:=11
LUCI_TITLE:=LuCI support for Nadkop
LUCI_DEPENDS:=+luci-base +nadkop
LUCI_PKGARCH:=all
PKG_LICENSE:=AGPL-3.0-or-later
PKG_MAINTAINER:=REALalphas

include $(TOPDIR)/feeds/luci/luci.mk

# Both APK and opkg/IPK are produced by the matching OpenWrt SDK.
# call BuildPackage - OpenWrt feed scanner signature (luci.mk builds the package).
# Note: ubus "nadkop get" stays in the write ACL (not read) because it
# returns subscription URLs; read-only LuCI users must not see it.
