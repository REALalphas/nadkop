# Nadkop interface

Styling follows the user's selected LuCI theme. The app uses standard
`cbi-tabmenu`, `cbi-section`, fields, and `cbi-button` buttons.
Custom CSS only handles element layout, width adaptation,
drag and drop, and icon sizes. All rules are scoped to `.nk-app`.
No custom palette, font, rounding, shadows, or light/dark theme mode is set.

Foreground: rules and connection selection, adding a subscription,
saving and applying. Middle ground: connection parameters and settings.
Background: diagnostics, service control, and extra parameters.
Tab order: Rules → Subscriptions → Settings → Import → Diagnostics.

Text holds only what the current action needs. Repetition,
technical detail with no decision value, and parenthetical explanations are
removed. Needed help shows behind "?" with the native LuCI hover/focus hint. Errors, service state, and action results stay visible.
Original provider node and separator names are kept.

The main action is "Save and apply". Other buttons use
the theme's neutral style. The selected route and pin state
shows in the theme's standard style with accessible labels.
The service is controlled from diagnostics; incompatible actions are never
shown together: start while the service is stopped, stop while it is running.

Emoji appearance stays the user's choice. Page styling
follows the LuCI theme; no separate Nadkop style is imposed.

Changes are checked against the Bootstrap, Material, and OpenWrt 2020 theme CSS on wide
and narrow screens, including expanded settings. Checks must confirm
node selection, help, and saving work, with no horizontal
overflow, inheriting the standard buttons' colors and shape.

```sh
NADKOP_PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
NADKOP_LUCI_THEME_ROOT=/path/to/sdk/feeds/luci/themes \
node scripts/test-picker.mjs
```

Rules can be renamed inline in the heading. Diagnostics shows
expandable steps: result, time, and error reason. Checks that did not run
are marked skipped. After drag/drop finishes, target frames disappear.

For a single server show only the connection. For several — a numbered
list, reordering, and the selection method: by availability, ping, or URL.
The URL field appears only for the URL probe. "Ping" is explained in the native
hint as HTTP latency via proxy. The node port is not shown in the card;
the subscription name is right-aligned and hidden when a specific subscription is selected.
Visible app and bypass-mechanism names are spelled Nadkop and Zapret.

The "Select server" button appears only for the "Server" action. That action
can be chosen before a connection is assigned. Deleting the last server leaves
the "Server" action selected with the select button available.

The add-server button and the selection method sit on one row above
the connection list. On a narrow screen the method label may
sit above its field, next to the add button.

Current layout (r21): arrow and title on the left of the heading, compact
pencil and trash on the right. The title edits only on request.
Collapsed state persists in the browser; new rules are open. Action type
does not change expansion. Under "Action" — the native "Server selection" row,
with the add button at its right edge. With one server the method is hidden.
The URL takes the full width of its field. No long explanations in rules or the header.
"?" sits right after "Connections". Pin and drag
handle are vertical; the flag has symmetric margins.
A subscription expands on its name click. Deleting a rule/subscription
asks for confirmation. Diagnostics shows the current process,
queue, and nftables state with reasons separate from the config check.

After a server is assigned, probe it immediately; the result goes in the card's bottom row
next to the subscription name and order arrows. A mode
or URL change restarts the probe; stale results are not shown.
The subscription name edits with the pencil; its ID and links stay unchanged.
The domains field accepts links and strips scheme and path on paste/blur.

A rule's conditions are split into "Domains", "IP / CIDR", "Sets".
Sets are picked through a search box with mixed/geosite/geoip filters;
the catalogue comes from the real .dat files. Countries are localized via Intl.
Picked sets show as short removable chips. Long
explanations and service enumerations are not printed in the rule.

Rule connection cards have an explicit number on the left, a colored status
with Lucide at top right. Status meaning is conveyed in text too. Bottom right
holds delete, up, and down; the subscription name sits left of them.
Flags ship in the package and are served by the router; the fallback is the system
emoji. Status color comes from theme variables with a semantic fallback.

A rule's server list collapses via a separate arrow left of the first
visible server. The list is indented left; the button stays in place
when the visible candidate changes.
Collapsed, the list keeps the candidate of the current selection method: first
available for priority, or fastest by ping/URL. With no probe
results the first server shows. List state persists in the browser
separate from rule expansion; the list is open by default.

In a collapsed list the number reads "2 of 5", showing the total
server count. The list's left indent is 2em, telling list and rule arrows apart.

Right of server adding is a compact re-probe icon.
During probing it spins and ignores repeat clicks.
The number column keeps equal width expanded and collapsed;
order buttons hide completely, delete stays right. Number and status are center-aligned to the
server row. Room for the page scrollbar is preserved.

Probing repeats transient errors up to three times with 1/2/3 s pauses.
One group's error does not skip the rest; the reason applies only
to that group's servers. A short reason shows in the status, the full text in
title. Pin and drag handle share one center.
In diagnostics the "State" block shows compact rows;
explanations appear only when an error is expanded.

Current layout (r31): order arrows in the top row next to
status and handle, delete in the bottom row right. Name and protocol
are vertically centered against both rows. Pin in the picker
sits above the handle with equal top/bottom margins.

In r32 the left and right rows are linked via CSS subgrid: name and status
in the first row, protocol/transport and subscription in the second. Row heights are shared,
so independent centering no longer shifts VLESS/KCP relative to
the subscription name.

In r33 the text subgrid gets an explicit stretch column: inherited
justify-content:center no longer centers name and protocol horizontally.
They sit left after the flag; shared row heights with the right part are kept.

Rule errors (r34): the rule number matches the UI numbering,
the error is bound to a specific rule and field. On save, the invalid
rule expands and focus moves to the field in error. An empty rule
asks to fill any one field: domains, IP/CIDR, sets. A selected
set without domains is a full condition. Missing
server and a set incompatible with Zapret are validated. A field error clears after
the fix. The server re-validates the installed sets before writing.

Conditions toggle separately via "Domains", "IP / CIDR",
"Sets" switches. Empty fields are hidden by default; filled ones are on.
Turning off excludes the values from rule operation, keeping
them in the config with the switch state. Turning back on
restores the values, including after save and page reload.
The empty-rule error prints at the switches.

The row is named "Condition". Domains/IP/sets toggle as buttons
with active state and aria-pressed, no checkboxes (LuCI r36).

The pin button aligns to the server card's top edge (r37):
the control column's top inner padding is removed.

A 1.5em gap is added before the bottom save block (r38).

With a mouse, rule edit and delete buttons show only on hover
over its heading or keyboard focus inside the heading.
Button slots keep their space without shifts. On touch screens the buttons show.

The proxy action is named "VPN", the mark on Zapret-incompatible sets is
"Xray only (VPN)". In diagnostics Xray headings read "Xray (VPN)".

In 0.3.0-r4, domain, IP/CIDR, and set intersections resolve by order:
the first matching rule from the top applies. Conditions inside a rule
combine with "or". This covers Zapret exclusions too.

An incompatible action or set enabling is unavailable with a short reason.
Compatibility follows the installed geodatabase content, including set
attributes; unsupported inverted sets cannot be added. A previously picked
incompatible set can be removed. Zapret needs a WAN device and a strategy.
Before saving, the server checks the draft without writing or restarting;
on save it checks again. The error points at a specific field.

When the action changes, the VPN selection and probe settings stay saved
but apply to VPN only. When the URL probe switches to ping, the address
persists and is unused. Inactive conditions are not validated as active
and do not reach the engine configs; on enabling they validate again.

In LuCI 0.3.0-r5 all four heading buttons (edit, up, down, delete)
share one square size and hide together outside heading hover/focus.
For Material, the theme's width auto!important is explicitly overridden. Button slots
persist; on touch screens they stay visible.

In LuCI 0.3.0-r6 post-click mouse focus no longer keeps heading buttons
visible: outside hover they show only on :focus-visible or while renaming.
CSS loads with a versioned URL, ruling out a stale arrow-size cache.
Refresh icons in server selection and the "Connections" heading match the
"Add server" height, textless; during a request they spin and ignore clicks.

In LuCI 0.3.0-r7 LAN and WAN use the standard tools.widgets.DeviceSelect
with network.getDevices() data: several devices for the home network, one
for the Zapret egress. Existing missing devices persist in the selection.
Linux device names store as before; network UCI is never written.
Settings group by purpose: device routing, subscriptions,
Zapret, routing databases, extra, appearance. Short explanations
show under fields. Internal Xray/DNS ports, upstream DNS, and local SOCKS
live in the "Extra" section, closed by default.

In LuCI 0.3.0-r8 DeviceSelect values read via instance.getValue():
LAN saves as a device-name array, WAN as a string. The event.detail
value/values fields hold LuCI label objects and never go to the config directly.
The Zapret-unavailable explanation no longer sticks next to a different
selected action; the reason stays on the disabled button. The subscription
interval keeps only values 0 and 1–168, without the phrase about previous connections.
