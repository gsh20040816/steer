include $(TOPDIR)/rules.mk

PKG_NAME:=steer
PKG_VERSION:=0.10.0
PKG_RELEASE:=1

PKG_LICENSE:=GPL-3.0-or-later
PKG_LICENSE_FILES:=LICENSE
PKG_MAINTAINER:=Steer contributors

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

GO_PKG:=github.com/gsh20040816/steer/go
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/steer-openwrt
GO_PKG_LDFLAGS_X:=main.version=$(PKG_VERSION)-r$(PKG_RELEASE)

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/steer
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Intent-driven transparent proxy controller for OpenWrt
  URL:=https://github.com/gsh20040816/steer
	DEPENDS:=$(GO_ARCH_DEPENDS) +ca-bundle +firewall4 +ip \
	+kmod-nft-queue +kmod-nft-tproxy +kmod-tun +sing-box
  PROVIDES:=steer-openwrt
  CONFLICTS:=steer-openwrt
  REPLACES:=steer-openwrt
endef

define Package/steer/description
 Steer validates strict routing and DNS intent, compiles the final sing-box
 configuration, and applies the required OpenWrt platform resources.
endef

define Package/steer/conffiles
/etc/config/steer
endef

define Package/steer/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0

config=/etc/config/steer
[ -f "$$config" ] || exit 0

cron_file=/etc/crontabs/root
cron_line='*/15 * * * * /usr/sbin/steer subscription update 2>&1 | logger -t steer-subscription'
[ -x /etc/init.d/cron ] || { echo 'steer: BusyBox crond is required' >&2; exit 1; }
mkdir -p /etc/crontabs
grep -Fqx "$$cron_line" "$$cron_file" 2>/dev/null || echo "$$cron_line" >> "$$cron_file"
/etc/init.d/cron restart

# Switch the schema 9 intent through Steer's verified generation transaction.
# PKG_UPGRADE must not leak into the init calls made by Apply.
apply_output="$$(PKG_UPGRADE=0 /usr/sbin/steer apply 2>&1)" || {
	echo "$$apply_output" >&2
	exit 1
}
endef

define Package/steer/prerm
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
cron_file=/etc/crontabs/root
cron_line='*/15 * * * * /usr/sbin/steer subscription update 2>&1 | logger -t steer-subscription'
if [ -f "$$cron_file" ]; then
	tmp="$${cron_file}.steer.$$"
	grep -Fvx "$$cron_line" "$$cron_file" > "$$tmp" || true
	mv "$$tmp" "$$cron_file"
	/etc/init.d/cron restart
fi
endef

define Build/Prepare
	$(call Build/Prepare/Default)
	$(CP) $(CURDIR)/../go/. $(PKG_BUILD_DIR)/
	$(CP) $(CURDIR)/../LICENSE $(PKG_BUILD_DIR)/
endef

define Package/steer/install
	$(call GoPackage/Package/Install/Bin,$(PKG_INSTALL_DIR))
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) $(PKG_INSTALL_DIR)/usr/bin/steer-openwrt $(1)/usr/sbin/steer

	$(INSTALL_DIR) $(1)/usr/share/steer
	$(CP) $(CURDIR)/../generated/geodata-seed $(1)/usr/share/steer/

	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/etc/config/steer $(1)/etc/config/steer

	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/etc/init.d/steer $(1)/etc/init.d/steer

	$(INSTALL_DIR) $(1)/lib/upgrade/keep.d
	$(INSTALL_DATA) ./files/lib/upgrade/keep.d/steer $(1)/lib/upgrade/keep.d/steer
endef

$(eval $(call GoBinPackage,steer))
$(eval $(call BuildPackage,steer))
