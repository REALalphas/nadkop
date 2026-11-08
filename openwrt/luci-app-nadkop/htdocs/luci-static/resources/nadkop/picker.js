'use strict';
'require baseclass';

// Lucide 0.468.0, ISC license; only the used SVG paths are shipped.
// See LICENSE.lucide. Flag SVGs ship locally under flags/ (MIT).
var iconShapes = {"pin":[["path",{"d":"M12 17v5"}],["path",{"d":"M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"}]],"pin-off":[["path",{"d":"M12 17v5"}],["path",{"d":"M15 9.34V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H7.89"}],["path",{"d":"m2 2 20 20"}],["path",{"d":"M9 9v1.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h11"}]],"grip-vertical":[["circle",{"cx":"9","cy":"12","r":"1"}],["circle",{"cx":"9","cy":"5","r":"1"}],["circle",{"cx":"9","cy":"19","r":"1"}],["circle",{"cx":"15","cy":"12","r":"1"}],["circle",{"cx":"15","cy":"5","r":"1"}],["circle",{"cx":"15","cy":"19","r":"1"}]],"map-pin":[["path",{"d":"M20 10c0 4.993-5.539 10.193-7.399 11.799a1 1 0 0 1-1.202 0C9.539 20.193 4 14.993 4 10a8 8 0 0 1 16 0"}],["circle",{"cx":"12","cy":"10","r":"3"}]],"refresh-cw":[["path",{"d":"M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"}],["path",{"d":"M21 3v5h-5"}],["path",{"d":"M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"}],["path",{"d":"M8 16H3v5"}]],"search":[["circle",{"cx":"11","cy":"11","r":"8"}],["path",{"d":"m21 21-4.3-4.3"}]],"server":[["rect",{"width":"20","height":"8","x":"2","y":"2","rx":"2","ry":"2"}],["rect",{"width":"20","height":"8","x":"2","y":"14","rx":"2","ry":"2"}],["line",{"x1":"6","x2":"6.01","y1":"6","y2":"6"}],["line",{"x1":"6","x2":"6.01","y1":"18","y2":"18"}]],"trash-2":[["path",{"d":"M3 6h18"}],["path",{"d":"M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"}],["path",{"d":"M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"}],["line",{"x1":"10","x2":"10","y1":"11","y2":"17"}],["line",{"x1":"14","x2":"14","y1":"11","y2":"17"}]],"plus":[["path",{"d":"M5 12h14"}],["path",{"d":"M12 5v14"}]],"arrow-right":[["path",{"d":"M5 12h14"}],["path",{"d":"m12 5 7 7-7 7"}]],"shield-ban":[["path",{"d":"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"}],["path",{"d":"m4.243 5.21 14.39 12.472"}]],"shield":[["path",{"d":"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"}]],"globe":[["circle",{"cx":"12","cy":"12","r":"10"}],["path",{"d":"M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"}],["path",{"d":"M2 12h20"}]],"circle-alert":[["circle",{"cx":"12","cy":"12","r":"10"}],["line",{"x1":"12","x2":"12","y1":"8","y2":"12"}],["line",{"x1":"12","x2":"12.01","y1":"16","y2":"16"}]],"x":[["path",{"d":"M18 6 6 18"}],["path",{"d":"m6 6 12 12"}]],"check":[["path",{"d":"M20 6 9 17l-5-5"}]],"arrow-right-left":[["path",{"d":"m16 3 4 4-4 4"}],["path",{"d":"M20 7H4"}],["path",{"d":"m8 21-4-4 4-4"}],["path",{"d":"M4 17h16"}]],"rss":[["path",{"d":"M4 11a9 9 0 0 1 9 9"}],["path",{"d":"M4 4a16 16 0 0 1 16 16"}],["circle",{"cx":"5","cy":"19","r":"1"}]],"settings-2":[["path",{"d":"M20 7h-9"}],["path",{"d":"M14 17H5"}],["circle",{"cx":"17","cy":"17","r":"3"}],["circle",{"cx":"7","cy":"7","r":"3"}]],"download":[["path",{"d":"M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"}],["polyline",{"points":"7 10 12 15 17 10"}],["line",{"x1":"12","x2":"12","y1":"15","y2":"3"}]],"save":[["path",{"d":"M15.2 3a2 2 0 0 1 1.4.6l3.8 3.8a2 2 0 0 1 .6 1.4V19a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z"}],["path",{"d":"M17 21v-7a1 1 0 0 0-1-1H8a1 1 0 0 0-1 1v7"}],["path",{"d":"M7 3v4a1 1 0 0 0 1 1h7"}]],"play":[["polygon",{"points":"6 3 20 12 6 21 6 3"}]],"square":[["rect",{"width":"18","height":"18","x":"3","y":"3","rx":"2"}]],"power":[["path",{"d":"M12 2v10"}],["path",{"d":"M18.4 6.6a9 9 0 1 1-12.77.04"}]]};
iconShapes['chevron-right'] = [['path', {d:'m9 18 6-6-6-6'}]];
iconShapes['chevron-up'] = [['path',{d:'m18 15-6-6-6 6'}]];
iconShapes['chevron-down'] = [['path', {d:'m6 9 6 6 6-6'}]];
iconShapes['arrow-up'] = [['path',{d:'m5 12 7-7 7 7'}],['path',{d:'M12 19V5'}]];
iconShapes['arrow-down'] = [['path',{d:'M12 5v14'}],['path',{d:'m19 12-7 7-7-7'}]];
iconShapes['circle-check'] = [['circle',{cx:12,cy:12,r:10}],['path',{d:'m9 12 2 2 4-4'}]];
iconShapes['circle-x'] = [['circle',{cx:12,cy:12,r:10}],['path',{d:'m15 9-6 6m0-6 6 6'}]];
iconShapes['loader-circle'] = [['path',{d:'M12 2a10 10 0 1 0 10 10'}]];
iconShapes.pencil = [['path', {d:'M17 3a2.83 2.83 0 0 1 4 4L7 21l-5 1 1-5Z'}], ['path', {d:'m15 5 4 4'}]];

function icon(name, className) {
 var ns = 'http://www.w3.org/2000/svg', svg = document.createElementNS(ns, 'svg');
 Object.entries({ viewBox: '0 0 24 24', width: '16', height: '16', fill: 'none', stroke: 'currentColor',
  'stroke-width': '1.8', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true', focusable: 'false',
  'class': 'nk-icon' + (className ? ' ' + className : '') }).forEach(function(a) { svg.setAttribute(a[0], a[1]); });
 (iconShapes[name] || iconShapes.server).forEach(function(shape) {
  var child = document.createElementNS(ns, shape[0]);
  Object.entries(shape[1]).forEach(function(a) { child.setAttribute(a[0], a[1]); }); svg.appendChild(child);
 });
 return svg;
}

var imageBase = 'https://cdn.jsdelivr.net/npm/emoji-datasource-apple@16.0.0/img/apple/64/';
var flagBase = (typeof L !== 'undefined' ? L.resource('nadkop/flags') : '/luci-static/resources/nadkop/flags') + '/';
var flagPattern = /[\u{1f1e6}-\u{1f1ff}]{2}/u;
var emojiPattern = /\p{Extended_Pictographic}|\p{Regional_Indicator}|\u20e3/u;
var segmenter = typeof Intl !== 'undefined' && Intl.Segmenter ? new Intl.Segmenter(undefined, { granularity: 'grapheme' }) : null;
var dragType = 'application/x-nadkop-server';
var countryNames = typeof Intl !== 'undefined' && Intl.DisplayNames ? new Intl.DisplayNames(['en'], { type: 'region' }) : null;
var englishNames = typeof Intl !== 'undefined' && Intl.DisplayNames ? new Intl.DisplayNames(['en'], { type: 'region' }) : null;
var regionCodes = ('AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW XK EU UN').split(' ');
var codeByName = {};
regionCodes.forEach(function(code) {
 [code, countryNames ? countryNames.of(code) : code, englishNames ? englishNames.of(code) : code].forEach(function(name) { codeByName[name.toLocaleLowerCase()] = code; });
});
function flagCode(value) {
 var match = (value || '').match(flagPattern);
 return match ? Array.from(match[0]).map(function(c) { return String.fromCharCode(c.codePointAt(0) - 0x1f1e6 + 65); }).join('') : '';
}
function country(value) {
 var server = typeof value === 'object' && value ? value : { name: value || '' };
 var loc = server.location || {}, name = server.name || server.tag || '';
 var code = (loc.countryCode || '').toUpperCase();
 if (code === 'UK') code = 'GB';
 if (regionCodes.indexOf(code) < 0) code = codeByName[(loc.country || '').toLocaleLowerCase()] || flagCode(loc.label) || flagCode(name);
 if (regionCodes.indexOf(code) < 0) code = '';
 var countryName = code ? (countryNames ? countryNames.of(code) : code) : (loc.country || '');
 var parts = [countryName, loc.city, loc.region].filter(function(part, i, all) { return part && all.indexOf(part) === i; });
 var label = loc.label || parts.join(' · ') || 'Location not specified';
 if (label === code) label = countryName;
 return { emoji: code ? Array.from(code).map(function(c) { return String.fromCodePoint(0x1f1e6 + c.charCodeAt(0) - 65); }).join('') : '',
  code: code, name: countryName || 'Location not specified', city: loc.city || '', region: loc.region || '', label: label };
}
function flag(region, style) {
 var fallback = E('span', { 'class': 'nk-flag-fallback nk-emoji-fallback' }, [region.code ? region.emoji : icon('globe')]);
 var children = [fallback];
 if (region.code) children.push(E('img', {
  src: flagBase + region.code.toLowerCase() + '.svg', alt: '', 'aria-hidden': 'true', width: '32', height: '24',
  loading: 'lazy', decoding: 'async', referrerpolicy: 'no-referrer', draggable: 'false',
  load: function() { fallback.style.visibility = 'hidden'; },
  error: function(ev) { ev.target.style.display = 'none'; fallback.style.visibility = ''; }
 }));
 return E('span', { 'class': 'nk-flag', role: 'img', 'aria-label': region.name, title: region.label }, children);
}
function emoji(value, style) {
 var fallback = E('span', { 'class': 'nk-emoji-fallback' }, [value]), children = [fallback];
 if (style !== 'system') {
  var points = Array.from(value).map(function(c) { return c.codePointAt(0).toString(16); });
  var unified = points.join('-'), retried = false;
  children.push(E('img', { src: imageBase + unified + '.png', alt: '', 'aria-hidden': 'true', width: '16', height: '16',
   loading: 'lazy', decoding: 'async', referrerpolicy: 'no-referrer', draggable: 'false',
   load: function() { fallback.style.visibility = 'hidden'; }, error: function(ev) {
    fallback.style.visibility = '';
    if (!retried && points.length <= 2) { retried = true; ev.target.src = imageBase + (points.indexOf('fe0f') >= 0 ? points.filter(function(p) { return p !== 'fe0f'; }).join('-') : unified + '-fe0f') + '.png'; }
    else { ev.target.style.display = 'none'; }
   }
  }));
 }
 return E('span', { 'class': 'nk-emoji', role: 'img', 'aria-label': value }, children);
}
function title(value, style) {
 value = value || '';
 if (!segmenter) return [value];
 return Array.from(segmenter.segment(value)).map(function(part) { return emojiPattern.test(part.segment) ? emoji(part.segment, style) : part.segment; });
}
function readDrag(event) { return event.dataTransfer ? event.dataTransfer.getData(dragType) || '' : ''; }

return baseclass.extend({
 icon: icon,
 dragType: dragType,
 country: country,
 readDrag: readDrag,
 allowDrop: function(event) {
  if (event.dataTransfer && Array.from(event.dataTransfer.types || []).indexOf(dragType) >= 0) { event.preventDefault(); event.dataTransfer.dropEffect = 'copy'; }
 },
 card: function(server, options) {
  options = options || {};
  var name = server.name || server.tag || 'Server', region = country(server);
  var accessibleName = options.accessibleName || name;
  var label = name.replace(flagPattern, '').replace(/^[\s|·:—-]+|[\s|·:—-]+$/g, '') || name;
  var available = server.available !== false;
  if (server.separator) return E('div', { 'class': 'nk-provider-separator', role: 'separator', 'aria-label': name,
   'data-server-id': server.id || '' }, [E('span', {}, title(name, options.emojiStyle))]);
  var transport = { raw: 'TCP', tcp: 'TCP', grpc: 'gRPC', splithttp: 'XHTTP' }[server.transport] || (server.transport || '').toUpperCase();
  var body = [flag(region, options.emojiStyle), E('div', { 'class': 'nk-server-copy' }, [
   E('div', { 'class': 'nk-server-title-row' }, [E('strong', { 'class': 'nk-server-name', title: name }, title(label, options.emojiStyle)),
    !available ? E('span', { 'class': 'nk-missing-text', title: 'Not in subscription · binding kept', 'aria-label': 'Not in subscription · binding kept' }, [icon('circle-alert')]) : '',
    options.usage ? E('span', { 'class': 'nk-server-usage', title: 'In rules: ' + options.usage, 'aria-label': 'In rules: ' + options.usage }, [String(options.usage)]) : ''
   ]),
   E('small', { 'class': 'nk-server-meta' }, [
    E('span', { 'class': 'nk-protocol' }, [(server.protocol || '').toUpperCase()]),
    transport ? E('span', { 'class': 'nk-transport' }, [transport]) : '',
    options.hideSource ? '' : E('span', { 'class': 'nk-source', title: options.sourceTitle || server.source || 'Local nodes' }, [options.sourceTitle || server.source || 'Local nodes'])
   ])
  ])];
  var select = E(options.onSelect ? 'button' : 'div', { type: options.onSelect ? 'button' : null,
   'class': 'nk-server-main' + (options.onSelect ? ' cbi-button ' + (options.selected ? 'cbi-button-action' : 'cbi-button-neutral') : ''), disabled: options.onSelect && !available ? true : null,
   'aria-label': options.onSelect ? 'Select ' + accessibleName + (options.selected ? ' · already in the selected rule' : '') : null,
   click: options.onSelect ? function() { if (available) options.onSelect(server.id); } : null }, body);
  var controls = (options.controls || []).slice();
  if (options.onPin) controls.push(E('button', { type: 'button', 'class': 'cbi-button ' + (options.pinned ? 'cbi-button-action' : 'cbi-button-neutral') + ' nk-pin' + (options.pinned ? ' is-pinned' : ''),
   title: options.pinned ? 'Unpin' : 'Pin', 'aria-label': (options.pinned ? 'Unpin ' : 'Pin ') + accessibleName,
   'aria-pressed': options.pinned ? 'true' : 'false', click: function(ev) { options.onPin(server.id, ev.currentTarget); } }, [icon(options.pinned ? 'pin-off' : 'pin')]));
  if (options.onRemove) controls.push(E('button', { type: 'button', 'class': 'cbi-button cbi-button-neutral nk-unlink', title: 'Remove from rule',
   'aria-label': 'Remove server from rule', click: options.onRemove }, [icon('x')]));
  if (options.draggable && available) controls.push(E('span', { 'class': 'nk-grip', title: 'Drag connection' }, [icon('grip-vertical')]));
  return E('article', { 'class': 'nk-server-card' + (available ? '' : ' is-missing') + (options.selected ? ' is-selected' : ''),
   'data-server-id': server.id || '', draggable: options.draggable && available ? 'true' : 'false',
   dragstart: function(ev) {
    if (!options.draggable || !available || (options.isBusy && options.isBusy())) { ev.preventDefault(); return; }
    ev.dataTransfer.effectAllowed = 'copyMove';
    // Only the stable reference crosses the drag boundary, never a node object.
    ev.dataTransfer.setData(dragType, server.id);
    ev.dataTransfer.setData('text/plain', server.id);
    ev.currentTarget.classList.add('is-dragging');
   },
   dragend: function(ev) { ev.currentTarget.classList.remove('is-dragging'); if (options.onDragEnd) options.onDragEnd(); },
   dragover: options.onReorder ? function(ev) {
    if (Array.from(ev.dataTransfer.types || []).indexOf(dragType) >= 0) { ev.preventDefault(); ev.stopPropagation(); ev.dataTransfer.dropEffect = 'move'; }
   } : null,
   drop: options.onReorder ? function(ev) {
    var id = readDrag(ev);
    if (id) { ev.preventDefault(); ev.stopPropagation(); options.onReorder(id, server.id); }
   } : null
  }, options.footer ? [select,E('div',{'class':'nk-assigned-top'},[options.trailing||'',E('div',{'class':'nk-card-controls'},controls)]),options.footer] :
   [select, options.trailing || '', E('div', { 'class': 'nk-card-controls' }, controls)]);
 }
});
