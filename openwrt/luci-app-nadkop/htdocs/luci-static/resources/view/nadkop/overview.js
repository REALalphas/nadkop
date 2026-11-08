'use strict';
'require view';
'require rpc';
'require ui';
'require poll';
'require form';
'require network';
'require tools.widgets as widgets';
'require nadkop.picker as picker';

var api = {};
[['get', []], ['validate',['config']], ['status', []], ['identity', []], ['save', ['config', 'nodes', 'revision']],
 ['geodata', []], ['updategeo', ['geodata']], ['probe', ['refs', 'mode', 'url']], ['zaprettune', ['action','wan','targets','protocols','revision','job','strategy']], ['import', ['content']], ['addsource', ['content', 'revision', 'options']], ['nodes', []], ['check', []], ['service', ['action']]].forEach(function(m) {
 api[m[0]] = rpc.declare({ object: 'nadkop', method: m[0], params: m[1], expect: { '': {} }, nobatch: m[0] === 'probe' || m[0] === 'zaprettune', reject: true });
});

function checked(promise) {
 return promise.then(function(result) {
  if (result.error) {var err=new Error(result.message||result.error);if(Number.isInteger(result.ruleIndex)){err.ruleIndex=result.ruleIndex;err.field=result.field||'matchers';}throw err;}
  return result;
 });
}
function input(value, change, type) {
 return E('input', { 'class': 'cbi-input-text', type: type || 'text', value: value == null ? '' : value,
  input: function(ev) { change(ev.target.value); } });
}
function area(value, change, rows) {
 return E('textarea', { 'class': 'cbi-input-textarea', rows: rows || 4,
  input: function(ev) { change(ev.target.value); } }, [value || '']);
}
function help(title, text) {
 return E('span', { 'class': 'cbi-tooltip-container nk-help', tabindex: '0',
  'aria-label': 'Help: ' + title, 'data-tooltip': text }, ['?']);
}
var fieldID = 0;
function field(title, control, text) {
 var id = 'nk-field-' + (++fieldID);
 if (control.matches('input,textarea,select')) { control.id = id; control.setAttribute('aria-label', title); }
 return E('div', { 'class': 'cbi-value', 'data-rule-field':({'Domains':'domains','IP / CIDR':'ips','Sets':'sets','Action':'action','Condition':'matchers','Check URL':'url','Server selection':'selection'}[title]||null) }, [E('label', { 'class': 'cbi-value-title', 'for': control.id || null }, [
  title, text ? help(title, text) : ''
 ]), E('div', { 'class': 'cbi-value-field' }, [control])]);
}
function select(value, options, change) {
 var el = E('select', { 'class': 'cbi-input-select', change: function(ev) { change(ev.target.value); } },
  options.map(function(o) { return E('option', { value: o[0], selected: o[0] === value ? '' : null }, [o[1]]); }));
 el.value = value;
 return el;
}
function button(text, action, primary) {
 return E('button', { 'class': 'cbi-button ' + (primary ? 'cbi-button-apply' : 'cbi-button-neutral'),
  type: 'button', click: action }, [text]);
}
function words(text) {
 return text.split(/[\s,]+/).filter(Boolean).map(function(value) {
  try { if (/^(?:https?:)?\/\//i.test(value)) return new URL(value.replace(/^\/\//,'https://')).hostname.toLowerCase(); } catch (e) {}
  return value.replace(/^https?:\/\//i,'').split(/[/?#]/)[0].replace(/:\d+$/,'').toLowerCase();
 }).filter(Boolean);
}

return view.extend({
 handleSave: null, handleSaveApply: null, handleReset: null,
 load: function() { return Promise.all([checked(api.get()), checked(api.status()),
  checked(api.identity()).catch(function(err) { return { error: err.message }; }), network.getDevices()]); },
 render: function(data) {
  var editorView = this;
  var savedState = JSON.parse(JSON.stringify(data[0]));
  var config = data[0].config, revision = data[0].revision;
  var identity = data[2] || {};
  var nodesText = JSON.stringify(data[0].nodes, null, 2), nodeTags = data[0].servers || [];
  config.picker = config.picker || {};
  config.picker.pinned = config.picker.pinned || []; config.picker.order = config.picker.order || [];
  var geoCatalogue = null, geoSettingsBox = E('div',{'class':'nk-geo-settings'});
  var probeQueue = [], probeRunning = false;
  var ruleViews = new WeakMap(), savedRuleViews = {}, savedServerViews = {};
  try { savedRuleViews = JSON.parse(localStorage.getItem('nadkop.rules.collapsed.v1') || '{}'); if (!savedRuleViews || typeof savedRuleViews !== 'object') savedRuleViews = {}; } catch (e) {}
  try { savedServerViews = JSON.parse(localStorage.getItem('nadkop.servers.collapsed.v1') || '{}'); if (!savedServerViews || typeof savedServerViews !== 'object') savedServerViews = {}; } catch (e) {}
  var selectedRule = -1, poolQuery = '', poolSource = '', onlyPinned = false;
  config.sources = config.sources || []; config.rules = config.rules || [];
  config.zapretArgs = config.zapretArgs || [];
	config.transparent = config.transparent || { enabled:false, interfaces:[], ipv6:true };
	function networkValue(key,value) { config.transparent[key]=value;changed(); }
  var deviceMap = new form.JSONMap({ settings:{} });
  var deviceSection = deviceMap.section(form.NamedSection,'settings','settings');
  function deviceSelector(key,value,multiple,change) {
   var option=deviceSection.option(widgets.DeviceSelect,key);
   option.devices=data[3]||[];option.networks=null;option.multiple=multiple;option.noaliases=true;option.optional=true;
   var control=option.renderWidget('settings',0,value);
   control.setAttribute('aria-label',multiple?'Home network devices':'Internet uplink device');
   control.addEventListener('cbi-dropdown-change',function(ev){
    var selected=ev.detail.instance.getValue();change(multiple?selected||[]:selected||'');
   });
   return control;
  }
  function settingField(title,control,description) {
   var row=field(title,control);
   if(description)row.querySelector('.cbi-value-field').appendChild(E('p',{'class':'cbi-value-description'},[description]));
   return row;
  }
  function settingsSection(title,description,content) {
   return E('section',{'class':'cbi-section nk-settings-section'},[E('h3',{},[title]),description?E('p',{},[description]):''].concat(content));
  }
  config.sources.forEach(function(s) { s._headers = JSON.stringify(s.headers || {}, null, 2); });
  var statusBox = E('small', { role: 'status' }), sourceBox = E('div'), ruleBox = E('div', { 'class': 'nk-rule-list' });
  var diagnosticBox = E('div', { 'class': 'nk-diagnostics', 'aria-live': 'polite' });
  var runtimeBox = E('ul', { 'class':'nk-runtime-checks', 'aria-live':'polite' });
  var serviceControls = E('div', { 'class': 'nk-toolbar' }), serviceInfo = E('div');
  var poolList = E('div', { 'class': 'nk-picker-list' });
  var poolTarget = E('small', { 'class': 'nk-picker-target', role: 'status' });
  var poolCount = E('small', { 'class': 'nk-picker-caption' });
  var poolFilters = E('div', { 'class': 'nk-picker-filters' });
  var dirty = E('small', { role: 'status', 'aria-live': 'polite' }, ['Saved']), busy = false;
  var validationAttempted=false, editRevision=0;
  var draftDirty = false;
  var draftObserver = new MutationObserver(function() {
   if (!root.isConnected) draftState(false);
  });
  function beforeUnload(ev) {
   if (!draftDirty || !root.isConnected) return;
   ev.preventDefault(); ev.returnValue = '';
  }
  function draftState(value) {
   draftDirty = value;
   window.removeEventListener('beforeunload', beforeUnload);
   draftObserver.disconnect();
   if (value) {
    window.addEventListener('beforeunload', beforeUnload);
    draftObserver.observe(document.documentElement, {childList:true,subtree:true});
   }
   resetButton.disabled = busy || !value;
   if (Object.prototype.hasOwnProperty.call(resetButton, '_nadkopDisabled')) resetButton._nadkopDisabled = !value;
  }
  var resetButton = button('Discard changes', function() {
   if (busy || !draftDirty) return;
   ui.showModal('Discard changes?', [E('p', {}, ['Return to the last saved state?']), E('div', { 'class':'right' }, [
    button('Cancel', function() { ui.hideModal(); }),
    button('Discard', function() {
     ui.hideModal();
     if (busy || !draftDirty) return;
     config.rules.forEach(function(rule) {
      var state = ruleView(rule); clearTimeout(state.probeTimer); state.probe = null;
     });
     probeQueue = []; draftState(false);
     if (poll.remove) { poll.remove(pollStatus); poll.remove(pollTune); }
     var replacement = editorView.render([JSON.parse(JSON.stringify(savedState)), data[1], data[2], data[3]]);
     root.replaceWith(replacement);
    })
   ])]);
  });
  resetButton.disabled = true;
  function changed() { editRevision++;draftState(true);dirty.textContent = 'Unsaved';paintActionCompatibility();if(validationAttempted)paintRuleErrors(); }
  function confirmDelete(title, apply) {
   ui.showModal(title, [E('p', {}, ['Remove from the configuration?']), E('div', { 'class':'right' }, [
    E('button', { type:'button', 'class':'cbi-button cbi-button-neutral', click:function() { ui.hideModal(); } }, ['Cancel']),
    E('button', { type:'button', 'class':'cbi-button cbi-button-negative', click:function() { ui.hideModal(); apply(); } }, ['Delete'])
   ])]);
  }
  function notifyError(err) {
   if(Number.isInteger(err.ruleIndex)&&config.rules[err.ruleIndex]) {
    var rule=config.rules[err.ruleIndex];ruleView(rule).validation={field:err.field,message:err.message,signature:JSON.stringify(rule)};
    validationAttempted=true;revealRuleError(err.ruleIndex);return;
   }
   ui.addNotification(null, E('p', {}, [err.message || String(err)]), 'error');
  }
  function lockControls() {
   if (!busy) return;
   root.querySelectorAll('.cbi-dropdown').forEach(function(el) {
    if (!Object.prototype.hasOwnProperty.call(el,'_nadkopInert')) el._nadkopInert=el.inert;
    el.inert=true;
   });
   root.querySelectorAll('button,input,textarea,select').forEach(function(control) {
    if (!Object.prototype.hasOwnProperty.call(control,'_nadkopDisabled')) control._nadkopDisabled=control.disabled;
    control.disabled=true;
   });
  }
  function task(fn) {
   if (busy) return Promise.resolve();
   busy = true;
   lockControls();
   return Promise.resolve().then(fn).catch(notifyError).finally(function() {
    busy = false; root.querySelectorAll('button,input,textarea,select').forEach(function(b) {
     if (Object.prototype.hasOwnProperty.call(b, '_nadkopDisabled')) { b.disabled = b._nadkopDisabled; delete b._nadkopDisabled; }
    });root.querySelectorAll('.cbi-dropdown').forEach(function(el){
     if(Object.prototype.hasOwnProperty.call(el,'_nadkopInert')){el.inert=el._nadkopInert;delete el._nadkopInert;}
    });paintActionCompatibility();tuneControls();
   });
  }
  var statusRendered = '';
  function status(s) {
   var instances = s.instances || {}, x = instances.xray, nf = instances.nfqws, running = x && x.running;
   var zapret = nf && nf.running ? 'running' : s.zapretRequired === false ? 'not used' : 'not running';
   var statusText = 'Xray: ' + (running ? 'running' : 'stopped') + ' · Zapret: ' + zapret;
   if (statusBox.textContent !== statusText) statusBox.textContent = statusText;
   var checks = (s.runtimeChecks || []).map(function(check) {
    return { id: check.id || check.title, title: check.title, state: check.state, detail: check.detail };
   });
   // Rebuilding hidden diagnostics on every poll closes an open details and
   // replaces focused buttons. Leave the DOM alone while the payload is stable.
   var signature = JSON.stringify([s.enabled, s.generatedAt, running, nf && nf.running, s.zapretRequired,
    x && x.pid, nf && nf.pid, instances.monitor && instances.monitor.pid, checks]);
   if (signature === statusRendered) return;
   statusRendered = signature;
   var openChecks = {}, focusedCheck = '';
   Array.prototype.forEach.call(runtimeBox.querySelectorAll('details'), function(el) {
    if (el.open) openChecks[el.getAttribute('data-check-id')] = true;
    if (el.contains(document.activeElement)) focusedCheck = el.getAttribute('data-check-id');
   });
   var activeButton = serviceControls.contains(document.activeElement) ? document.activeElement.textContent : '';
   domReplace(runtimeBox, checks.map(function(check) {
    var line=[E('strong',{},[check.title==='Xray'?'Xray (VPN)':check.title]),E('span',{},[picker.icon({ok:'circle-check',success:'circle-check',warning:'circle-alert',error:'circle-x',info:'circle-alert',pending:'loader-circle',unknown:'circle-alert'}[check.state]||'circle-alert'),({ok:'Running',success:'Completed',warning:'Warning',error:'Error',info:'Not used',pending:'In progress',unknown:'No data'}[check.state]||check.state)])];
    return E('li',{'data-state':check.state},[['error','success','pending','unknown','warning'].indexOf(check.state)>=0?E('details',{'data-check-id':check.id,open:openChecks[check.id]?'':null},[E('summary',{'class':'nk-runtime-line'},line),E('p',{},[check.detail])]):E('div',{'class':'nk-runtime-line'},line)]);
   }));
   if (focusedCheck) Array.prototype.some.call(runtimeBox.querySelectorAll('details'), function(el) {
    if (el.getAttribute('data-check-id') !== focusedCheck) return false;
    var summary = el.querySelector('summary');
    if (summary) summary.focus({preventScroll:true});
    return true;
   });
   domReplace(serviceInfo, [E('p', {}, ['Autostart: ' + (s.enabled ? 'enabled' : 'disabled')]),
    E('small', {}, ['Updated: ' + (s.generatedAt ? new Date(s.generatedAt * 1000).toLocaleString() : '—')])]);
   domReplace(serviceControls, [
    running ? button('Restart', function() { return act('restart'); }) : button('Start', function() { return act('start'); }),
    running ? button('Stop', function(ev) { confirmService('stop', ev.currentTarget); }) : '',
    button(s.enabled ? 'Disable autostart' : 'Enable autostart', function(ev) { return s.enabled ? confirmService('disable', ev.currentTarget) : act('enable'); })
   ].filter(Boolean));
   serviceControls.querySelectorAll('button').forEach(function(b) {
    if (b.textContent === 'Stop') b.setAttribute('data-service-action', 'stop');
    if (b.textContent === 'Disable autostart') b.setAttribute('data-service-action', 'disable');
   });
   if (activeButton) Array.prototype.some.call(serviceControls.querySelectorAll('button'), function(b) {
    if (b.textContent !== activeButton || b.disabled) return false;
    b.focus({preventScroll:true});
    return true;
   });
  }
  function refresh() { return checked(api.status()).then(status); }
  function act(action) { return task(function() { return checked(api.service(action)).then(refresh); }); }
  function confirmService(action, control) {
   if (busy) return;
   var stop = action === 'stop';
   ui.showModal(stop ? 'Stop the service?' : 'Disable autostart?', [
    E('p', {}, [stop ? 'Stopping the service will interrupt transparent LAN traffic interception.' : 'The service will not start automatically. The currently running service will continue.']),
    E('div', { 'class':'right' }, [
     button('Cancel', function() {
      ui.hideModal();
      restoreActionFocus(control, serviceControls, 'button[data-service-action="' + action + '"]', 'button');
     }),
     E('button', { type:'button', 'class':'cbi-button cbi-button-negative', click:function() {
      if (busy) return;
      ui.hideModal();
      return act(action);
     } }, [stop ? 'Stop' : 'Disable'])
    ])
   ]);
  }
  function domReplace(el, children) { while (el.firstChild) el.removeChild(el.firstChild); children.forEach(function(c) { el.appendChild(c); }); lockControls(); }
  // Tuning has its own short RPC operations. A background run must leave the
  // cancel/status controls available, and must never silently edit the draft.
  var tuneJob=null,tunePending=false,tuneRefreshing=false,tuneKnown=false,tuneSites='',tuneProtocols=['https'],tuneRendered='';
  var tuneState=E('p',{role:'status','aria-live':'polite'}),tuneResults=E('div'),tuneProgress=E('progress',{'aria-label':'Network probes completed',value:0,max:1,hidden:''});
  var tuneSitesEditor=area('',function(v){tuneSites=v;},4);
  tuneSitesEditor.setAttribute('placeholder','example.org\nexample.net');
  var tuneProtocolEditor=select('https',[['https','HTTPS · TLS, TCP 443'],['both','HTTP and HTTPS · TCP 80/443']],function(v){tuneProtocols=v==='both'?['http','https']:['https'];});
  function tuneActive() {return tuneJob&&['queued','running','restoring'].indexOf(tuneJob.status)>=0;}
  function tuneCall(action,job,strategy){return checked(api.zaprettune(action,config.wanInterface||'',tuneSites.split(/[\s,]+/).filter(Boolean),tuneProtocols,revision,job||'',strategy||''));}
  function tuneResponse(result,allowEmpty){
   if(!result||!Object.prototype.hasOwnProperty.call(result,'job')||(!allowEmpty&&result.job===null))throw new Error('No confirmed task state');
   var j=result.job;
   if(j!==null&&(!j||!/^[0-9a-f]{32}$/.test(j.id||'')||['queued','running','restoring','completed','unneeded','no_strategy','inconclusive','failed','cancelled','recovery_failed'].indexOf(j.status)<0||typeof j.message!=='string'||typeof j.restored!=='boolean'||!Array.isArray(j.results)||!Array.isArray(j.strategies)||!Number.isFinite(j.completed)||!Number.isFinite(j.total)))throw new Error('Corrupted task state response');
   return j;
  }
  // Writing an unchanged boolean still emits DOM mutations; a 2 s poll doing
  // that to visible controls shows up as flicker in some browsers.
  function tuneSet(el,key,value){if(el[key]!==value)el[key]=value;}
  function tuneControls(){
   tuneSet(tuneStart,'disabled',busy||tunePending||!tuneKnown||!!tuneActive()||!!(tuneJob&&!tuneJob.restored));
   tuneSet(tuneCancel,'disabled',tunePending||(tuneKnown&&(!tuneJob||(!tuneActive()&&tuneJob.restored))));
   tuneSet(tuneRefresh,'disabled',tunePending);
   tuneSet(tuneChoose,'disabled',busy||tunePending||!tuneKnown||!tuneJob||tuneJob.status!=='completed'||!tuneJob.restored||!tuneJob.winner);
   tuneSet(tuneChoose,'hidden',!(tuneJob&&tuneJob.status==='completed'&&tuneJob.restored&&tuneJob.winner));
   tuneSet(tuneSitesEditor,'disabled',tunePending||!!tuneActive());
   tuneSet(tuneProtocolEditor,'disabled',tuneSitesEditor.disabled);
  }
  function showTune(job){
   tuneJob=job||null;tuneKnown=true;
   tuneState.textContent=job?job.message+(job.restored?' Temporary resources have been removed.':job.status==='restoring'?' Cleanup is not confirmed yet.':''):'Tuning has not been started yet.';
   tuneProgress.hidden=!job||!tuneActive();
   if(job){tuneProgress.max=Math.max(1,job.total||1);tuneProgress.value=job.completed||0;tuneProgress.setAttribute('aria-valuetext','Completed '+(job.completed||0)+' probes out of '+(job.total||0));}
   var signature=JSON.stringify(job?[job.id,job.status,job.completed,job.winner]:null);
   if(signature===tuneRendered){tuneControls();return;}
   tuneRendered=signature;
   var previousDetails=tuneResults.querySelector('details'),detailsOpen=previousDetails&&previousDetails.open;
   var focusedSummary=previousDetails&&document.activeElement===previousDetails.querySelector('summary');
   var previousList=tuneResults.querySelector('ul'),listScroll=previousList?previousList.scrollTop:0;
   var content=[];
   if(job){
    content.push(E('p',{},['Check '+new Date(job.startedAt*1000).toLocaleString()+' · WAN: '+job.wan+' · probes: '+(job.completed||0)]));
    if(job.results&&job.results.length){
     var titles={};(job.strategies||[]).forEach(function(s){titles[s.id]=s.title;});
     content.push(E('details',{},[E('summary',{},['Per-site and per-protocol results ('+job.results.length+')']),E('ul',{'class':'nk-tune-results'},job.results.map(function(r){
      var stage=r.stage==='baseline'?'No strategy':r.stage==='baseline_recheck'?'No-strategy recheck':(titles[r.strategy]||r.strategy)+' · attempt '+r.round;
      var reason={timeout:'timed out',connection_or_tls:'connection or TLS error',tls_certificate:'TLS certificate error',http_status_or_body:'HTTP or response read error',queue_not_used:'nfqws processing not confirmed'}[r.code]||r.code;
      return E('li',{},[E('strong',{},[r.host+' · '+r.protocol.toUpperCase()]),E('span',{},[stage]),E('span',{},[(r.ok?'Available':'Failed: '+reason)+(r.httpStatus?' · HTTP '+r.httpStatus:'')+' · '+r.delayMs+' ms'+(r.strategy?(r.queued?' · nfqws confirmed':' · nfqws not confirmed'):'')])]);
     }))]));
    }
    if(job.winner){var winner=(job.strategies||[]).find(function(s){return s.id===job.winner;});if(winner)content.push(E('pre',{'class':'nk-tune-args'},[winner.args.join('\n')]));}
   }
   domReplace(tuneResults,content);
   var nextDetails=tuneResults.querySelector('details');if(nextDetails){nextDetails.open=!!detailsOpen;if(focusedSummary)nextDetails.querySelector('summary').focus({preventScroll:true});}
   var nextList=tuneResults.querySelector('ul');if(nextList)nextList.scrollTop=listScroll;
   tuneControls();
  }
  function tuneOperation(fn){
   if(tunePending)return Promise.resolve();tunePending=true;tuneControls();
   return Promise.resolve().then(fn).catch(function(err){tuneKnown=false;tuneState.textContent='No tuner response: '+err.message+'. The task may still be running. Refresh the state or send a cancellation.';notifyError(err);})
    .finally(function(){tunePending=false;tuneControls();});
  }
  function refreshTune(){
   // Background status polling must not disable the draft fields: disabling a
   // focused textarea blurs it, so a 2 s poll made typing impossible.
   if(tunePending||tuneRefreshing)return Promise.resolve();
   tuneRefreshing=true;tuneControls();
   return tuneCall('status').then(function(result){
    if(!tunePending)showTune(tuneResponse(result,true));
   }).catch(function(err){
    tuneKnown=false;tuneState.textContent='No tuner response: '+err.message+'. The task may still be running. Refresh the state or send a cancellation.';
   }).finally(function(){tuneRefreshing=false;tuneControls();});
  }
  var tuneStart=button('Start tuning',function(){
   if(busy)return;return tuneOperation(function(){return tuneCall('start').then(function(result){showTune(tuneResponse(result,false));});});
  },true);
  var tuneCancel=button('Cancel tuning',function(){return tuneOperation(function(){var id=tuneKnown&&tuneJob?tuneJob.id:'';return tuneCall('cancel',id).then(function(result){if(result.ok!==true)throw new Error('Cancellation not confirmed');tuneState.textContent='Cancellation requested. Waiting for confirmation that temporary resources were removed.';return tuneCall('status',id).then(function(result){showTune(tuneResponse(result,true));});});});});
  var tuneRefresh=button('Refresh tuner state',refreshTune);
  var tuneChoose=button('Apply the verified strategy to settings',function(){
   if(busy||tunePending||!tuneJob)return;
   var selected=tuneJob;
   ui.showModal('Replace the manual strategy in the draft?',[
    E('p',{},['Only the selected sites were checked over IPv4, HTTP/HTTPS via WAN '+selected.wan+'. Manual arguments will be replaced in the current draft. To start the service, then click "Save and apply".']),
    E('div',{'class':'right'},[button('Cancel',function(){ui.hideModal();}),button('Replace in draft',function(){
     ui.hideModal();var selectionRevision=editRevision;return tuneOperation(function(){return tuneCall('select',selected.id,selected.winner).then(function(result){
      if(!Array.isArray(result.args)||!result.args.length||result.wan!==config.wanInterface)throw new Error('The result does not match the current WAN');
      if(editRevision!==selectionRevision)throw new Error('The draft changed during selection; repeat the strategy transfer');
      config.zapretArgs=result.args.slice();zapretEditor.value=config.zapretArgs.join('\n');changed();
      tuneState.textContent='The verified strategy has been moved to the draft. To apply it, click "Save and apply".';
     });});
    },true)])]);
  });
  var tuneBox=E('div',{'class':'nk-tuner'},[
   E('h4',{},['Automatic strategy tuning']),
   E('p',{},['Up to 6 minutes. Probes run from the router via the selected WAN. Working rules and manual arguments are preserved. You can close the page; the task will continue and remove temporary resources.']),
   settingField('Tuning sites',tuneSitesEditor,'1 to 4 public domains, one per line. No URLs, IPs, paths, or parameters.'),
   settingField('Tuning protocols',tuneProtocolEditor,'IPv4 and TCP only. An HTTP 200–399 response is expected; for HTTPS the TLS certificate is verified too. QUIC/HTTP3, IPv6, and LAN access are not verified by this check.'),
   E('div',{'class':'nk-toolbar'},[tuneStart,tuneCancel,tuneRefresh,tuneChoose]),tuneState,tuneProgress,tuneResults
  ]);
  var zapretEditor=area(config.zapretArgs.join('\n'),function(v){config.zapretArgs=v.split('\n').map(function(a){return a.trim();}).filter(Boolean);changed();paintActionCompatibility();},7);
  tuneControls();
  function restoreActionFocus(previous, scope, selector, fallback) {
   if (!previous || (document.activeElement !== document.body && document.activeElement !== previous)) return;
   var control = scope && scope.querySelector(selector);
   if (control && (control.disabled || control.closest('[hidden]'))) control = null;
   if (!control) control = scope && scope.querySelector(fallback);
   if (control) control.focus({preventScroll:true});
  }
  function save(apply) {
   if(busy)return Promise.resolve();
   validationAttempted=true;paintRuleErrors();
   var invalid=config.rules.findIndex(function(rule){return !!ruleIssue(rule);});
   if(invalid>=0){revealRuleError(invalid);return Promise.resolve();}
   return task(function() {
    var savedEditRevision = editRevision, clean = JSON.parse(JSON.stringify(config));
    clean.sources.forEach(function(s, i) {
     s.headers = JSON.parse(config.sources[i]._headers || '{}'); delete s._headers;
     if (s.file) { delete s.url; delete s.headers; delete s.hwid; delete s.format; } else { delete s.file; }
    });
    var nodes = JSON.parse(nodesText);
    if (!Array.isArray(nodes)) throw new Error('Local nodes must be a JSON array');
    var phase = 'validate';
    return checked(api.validate(clean)).then(function() {
     phase = 'save';
     return checked(api.save(clean,nodes,revision)).then(function(result) {
      if (!result || typeof result.revision !== 'string' || !result.revision) throw new Error('Save not confirmed: missing configuration revision.');
      return result;
     });
    }).then(function(result) {
     phase = 'saved';
     revision = result.revision;
     savedState = JSON.parse(JSON.stringify({config:clean,nodes:nodes,revision:revision,servers:result.servers || nodeTags}));
     rememberRuleViews(); if(editRevision===savedEditRevision){draftState(false);dirty.textContent = 'Saved';}
     if (result.servers) { nodeTags = result.servers; rules(); renderPoolFilters(); renderPool(); }
     if (apply) {
      phase = 'service';
      return checked(api.service('restart')).then(function() {
       phase = 'applied';
       if(editRevision===savedEditRevision)dirty.textContent = 'Saved and applied';
       return refresh();
      });
     }
    }).catch(function(err) {
     var newer = editRevision!==savedEditRevision;
     if (phase === 'save') dirty.textContent = 'Save not confirmed' + (newer ? ' · there are new changes' : '');
     else if (phase === 'service') dirty.textContent = (newer ? 'Unsaved · ' : 'Saved, ') + 'apply not confirmed';
     throw err;
    });
   });
  }
  function sourceHost(value) { try { return new URL(value).hostname; } catch (_) { return 'Subscription'; } }
  var sourceInput, sourceMessage = E('p', { 'class': 'nk-source-message', role: 'status', 'aria-live': 'polite' });
  var sourceHeaders = '{}', sourceHWID = true;
  function insertSource(location) {
   location = location.trim();
   if (!location) return Promise.resolve();
   var options = { headers: JSON.parse(sourceHeaders), hwid: { enabled: sourceHWID } };
   sourceMessage.textContent = 'Loading subscription…'; sourceMessage.classList.remove('is-error');
   return checked(api.addsource(location, revision, options)).then(function(result) {
    revision = result.revision;
    var source = result.source;
    if (!config.sources.some(function(s) { return s.name === source.name; })) {
     source._headers = JSON.stringify(source.headers || {}, null, 2); config.sources.push(source);
    }
    nodeTags = result.servers || nodeTags;
    savedState.revision = revision; savedState.servers = JSON.parse(JSON.stringify(nodeTags));
    if (!savedState.config.sources.some(function(s) { return s.name === source.name; }))
     savedState.config.sources.push(JSON.parse(JSON.stringify(source)));
    sourceInput.value = '';
    sourceMessage.textContent = result.existing ? 'Already added: ' + source.name : 'Added: ' + source.name;
    sources(); rules(); renderPoolFilters(); renderPool();
   }).catch(function(err) {
    sourceMessage.textContent = err.message || String(err); sourceMessage.classList.add('is-error'); throw err;
   });
  }
  function submitSource() {
   if (busy || !sourceInput.value.trim()) return;
   return task(function() { return insertSource(sourceInput.value); });
  }
  sourceInput = E('input', { type: 'text', 'class': 'cbi-input-text nk-source-input',
   'aria-label': 'Subscription link or JSON path', placeholder: 'https://… or /etc/nadkop/provider.json',
   autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false',
   paste: function() { window.setTimeout(submitSource, 0); },
   change: submitSource, blur: submitSource,
   keydown: function(ev) { if (ev.key === 'Enter') { ev.preventDefault(); submitSource(); } }
  });
  function renderDiagnostics(result) {
   var labels = { ok: 'Done', error: 'Error', warning: 'Warning', skipped: 'Skipped', pending: 'Pending' };
   domReplace(diagnosticBox, [E('p', { role: 'status' }, [result.ok ? 'Check passed' + ((result.steps || []).some(function(s) { return s.state === 'warning'; }) ? ' with warnings' : '') : 'Check failed']),
    E('ol', { 'class': 'nk-diagnostic-steps' }, (result.steps || []).map(function(step) {
     var row = E('details', { 'data-step': step.id, 'data-state': step.state }, [
      E('summary', {}, [(step.title==='Xray'?'Xray (VPN)':step.title) + ' · ' + (labels[step.state] || step.state)]),
      E('p', {}, [step.detail || '—']), E('small', {}, [String(step.durationMs || 0) + ' ms'])
     ]);
     row.open = step.state === 'error' || step.state === 'warning';
     return E('li', {}, [row]);
    }))]);
  }
  function diagnose() {
   return task(function() {
    domReplace(diagnosticBox, [E('p', { role: 'status' }, ['Checking…'])]);
    return api.check().then(function(result) {
     if (!Array.isArray(result.steps)) throw new Error(result.error || 'Could not get check results');
     renderDiagnostics(result);
    }).catch(function(err) {
     domReplace(diagnosticBox, [E('p', { role: 'alert' }, [err.message || String(err)])]);
     throw err;
    });
   });
  }
  function sourceTitle(name) {
   var source = config.sources.find(function(s) { return s.name === name; });
   return source && source.title || name;
  }
  function renameSource(source) {
   if(busy)return;
   var editor = input(source.title || source.name,function() {});
   editor.setAttribute('maxlength','80'); editor.setAttribute('aria-label','Subscription name');
   function commit() {
    if(busy)return;
    var title = editor.value.trim();
    if (!title || title.length>80) { editor.focus(); return; }
    source.title = title; changed(); ui.hideModal(); sources(); rules(); renderPoolFilters(); renderPool();
   }
   editor.addEventListener('keydown',function(ev) { if(ev.key==='Enter') {ev.preventDefault();commit();} });
   ui.showModal('Subscription name',[editor,E('div',{'class':'right'},[
    button('Cancel',function(){ui.hideModal();}),button('Save',commit,true)
   ])]); editor.focus(); editor.select();
  }
  function sources() {
   var expanded = Array.from(sourceBox.querySelectorAll('details')).map(function(d) { return d.open; });
   domReplace(sourceBox, config.sources.map(function(s, i) {
    var hwid = s.hwid || { enabled: true };
    function deviceValue(key, value) { if (!s.hwid) s.hwid = { enabled: true }; s.hwid[key] = value; changed(); }
    function deviceInput(key, fallback) {
     var control = input(hwid[key], function(v) { deviceValue(key, v); });
     control.setAttribute('placeholder', fallback || 'Automatically');
     return control;
    }
    return E('section', { 'class': 'cbi-section nk-source-section' }, [
     E('details', {}, [E('summary', { 'class':'nk-source-heading' }, [E('strong', {}, [sourceTitle(s.name)]),
      E('small', {}, [(s.file ? s.file : sourceHost(s.url)) + ' · ' + nodeTags.filter(function(n) { return n.source === s.name && n.available && !n.separator; }).length]),
      E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-action nk-source-rename','aria-label':'Rename subscription ' + sourceTitle(s.name),click:function(ev){ev.preventDefault();ev.stopPropagation();renameSource(s);}},[picker.icon('pencil')])]),
     s.file ? field('JSON file', input(s.file, function(v) { s.file = v; changed(); }), 'Absolute path on the router, starting with /.') :
      field('Subscription URL', input(s.url, function(v) { s.url = v; changed(); }, 'password')),
     s.file ? E('span') : field('HTTP headers', area(s._headers, function(v) { s._headers = v; changed(); }, 3), 'Example: {"Authorization":"Bearer …","User-Agent":"…"}'),
     s.file ? E('span') : field('Subscription format', select(s.format || 'xray-json', [['xray-json', 'Xray JSON'], ['links', 'Links / base64']], function(v) {
      s.format = v; changed();
     }), 'Xray JSON preserves connection parameters. Choose links if the provider does not serve JSON.'),
     s.file ? E('span') : field('Send HWID', select(hwid.enabled ? 'on' : 'off', [['off', 'Disabled'], ['on', 'Enabled']], function(v) {
      if (!s.hwid) s.hwid = {};
      s.hwid.enabled = v === 'on'; changed(); sources();
     }), 'Device identifier at the provider. A custom X-HWID header takes priority.'),
     !s.file && hwid.enabled ? E('div', {}, [
      field('Router HWID', identity.id ? E('code', { 'class': 'nk-identity' }, [identity.id]) : E('span', {}, [identity.error || 'HWID unavailable']),
       'Persistent router identifier. It survives upgrades and backups.'),
      field('Custom HWID', deviceInput('id', identity.id), 'Leave empty for the router HWID. 10–64 Latin letters, digits, and = or - characters are allowed.'),
      field('Device OS', deviceInput('os', identity.os)),
      field('OS version', deviceInput('version', identity.version)),
      field('Device model', deviceInput('model', identity.model))
     ]) : E('span'),
     button('Delete subscription', function() { confirmDelete('Delete subscription "' + s.name + '"?',function() { config.sources.splice(i,1); changed(); sources(); }); })
     ])
    ]);
   }));
   sourceBox.querySelectorAll('details').forEach(function(d, i) { d.open = !!expanded[i]; });
  }
  function ruleRefs(rule) { return rule.nodeRefs || (rule.nodeRef ? [rule.nodeRef] : []); }
  function serverByRef(ref) { return nodeTags.find(function(s) { return s.id === ref; }) || { id:ref, name:'Server unavailable', protocol:'', source:'', available:false }; }
  function serversForRule(rule) {
   if (ruleRefs(rule).length) return ruleRefs(rule).map(serverByRef);
   if (rule.node) {
    var matches = nodeTags.filter(function(s) { return !s.separator && (s.tag === rule.node || (s.aliases || []).indexOf(rule.node) >= 0); });
    return matches.length === 1 ? matches : [{ id:'', name:rule.node, protocol:'', source:rule.node.split('/')[0], available:false }];
   }
   return [];
  }
  function setRuleRefs(rule, refs) {
   delete rule.node; delete rule.nodeRef; delete rule.nodeRefs;
   if (refs.length > 1) { rule.nodeRefs = refs; rule.selection = rule.selection || { mode:'priority' }; }
   else { if (refs.length) rule.nodeRef = refs[0]; }
   rule.action = 'proxy';
  }
  function removeServer(index, memberIndex) {
   if(busy)return;
   var rule = config.rules[index], refs = serversForRule(rule).map(function(s) { return s.id; });
   refs.splice(memberIndex,1); setRuleRefs(rule,refs); changed(); rules(); renderPool(); if(refs.length)checkRule(rule);
  }
  function moveServer(index, from, to, control) {
   if(busy)return;
   control = document.activeElement === control ? control : null;
   var rule = config.rules[index], refs = ruleRefs(rule).slice();
   if (to<0 || to>=refs.length) return;
   var ref = refs.splice(from,1)[0]; refs.splice(to,0,ref); setRuleRefs(rule,refs); changed(); rules(); renderPool();
   var section = ruleBox.querySelector('[data-rule-index="' + index + '"]');
   var card = section && Array.from(section.querySelectorAll('.nk-server-card')).find(function(el) { return el.getAttribute('data-server-id') === ref; });
   restoreActionFocus(control, card, '[data-move-server="' + (to < from ? 'up' : 'down') + '"]:not(:disabled)', '.nk-move-server:not(:disabled)');
  }
  function reorderServer(index, id, target) {
   if(busy)return;
   var refs = ruleRefs(config.rules[index]), from = refs.indexOf(id), to = refs.indexOf(target);
   if (from < 0) assignServer(id,index);
   else if (to >= 0 && from !== to) moveServer(index,from,to);
   clearDropState();
  }
  function ruleViewKey(rule, index) {
   var signature = JSON.stringify([(rule.domains || []).slice().sort(),(rule.ips||[]).slice().sort(),(rule.sets||[]).slice().sort()]), occurrence = 0;
   for (var n = 0; n < index; n++) if (JSON.stringify([(config.rules[n].domains || []).slice().sort(),(config.rules[n].ips||[]).slice().sort(),(config.rules[n].sets||[]).slice().sort()]) === signature) occurrence++;
   return signature + ':' + occurrence;
  }
  function ruleView(rule) {
   if (!ruleViews.has(rule)) ruleViews.set(rule, { collapsed:savedRuleViews[ruleViewKey(rule,config.rules.indexOf(rule))] === true, serversCollapsed:savedServerViews[ruleViewKey(rule,config.rules.indexOf(rule))] === true, editing:false, draft:'' });
   return ruleViews.get(rule);
  }
  function rememberRuleViews() {
   var next = {}, serverNext = {};
   config.rules.forEach(function(rule,index) { var state=ruleView(rule),key=ruleViewKey(rule,index);if (state.collapsed) next[key] = true; if(state.serversCollapsed)serverNext[key]=true; });
   savedRuleViews = next; savedServerViews=serverNext;
   try { localStorage.setItem('nadkop.rules.collapsed.v1',JSON.stringify(next)); localStorage.setItem('nadkop.servers.collapsed.v1',JSON.stringify(serverNext)); } catch (e) {}
  }
  function preferredServerIndex(rule) {
   var chosen=serversForRule(rule),probe=ruleView(rule).probe,mode=(rule.selection||{}).mode||'priority';
   if(!probe || probe.mode!==mode || probe.error)return 0;
   var best=-1;
   chosen.forEach(function(server,index){
    var result=probe.results&&probe.results[server.id];
    if(!server.available||!result||!result.available)return;
    if(best<0 || mode!=='priority'&&result.delayMs<probe.results[chosen[best].id].delayMs)best=index;
   });
   return best<0?0:best;
  }
  function updateServerVisibility(rule) {
   var index=config.rules.indexOf(rule);if(index<0)return;
   var section=ruleBox.querySelector('[data-rule-index="'+index+'"]');if(!section)return;
   var list=section.querySelector('.nk-rule-servers');if(!list)return;
   var collapsed=ruleView(rule).serversCollapsed,preferred=preferredServerIndex(rule);
   list.setAttribute('data-collapsed',String(collapsed));
   Array.from(list.children).forEach(function(row,i){
    row.hidden=collapsed&&i!==preferred;
    var position=row.querySelector('.nk-server-position'),label=collapsed&&list.children.length>1?(i+1)+' of '+list.children.length:String(i+1);
    position.textContent=label;position.setAttribute('aria-label',collapsed&&list.children.length>1?'Server '+label:'Order '+(i+1));
   });
  }
  function probeKey(rule) { return JSON.stringify([ruleRefs(rule),(rule.selection || {}).mode || 'priority',(rule.selection || {}).url || '']); }
  function probeErrorText(message) {
   if(/Probe already running/.test(message))return 'Probe busy';
   if(/load servers/.test(message))return 'Subscription unavailable';
   if(/timeout|timed out|expired/i.test(message))return 'Probe timed out';
   if(/Xray/.test(message))return 'Xray launch error';
   return message||'No response from the router';
  }
  function probeText(rule,id) {
   var state = ruleView(rule).probe;
   if (!state) return '';
   var result = state.results && state.results[id];
   if (!result) return state.errors&&state.errors[id]?probeErrorText(state.errors[id]):state.pending?'Checking…':state.error?probeErrorText(state.error):'';
   return result.available ? (state.mode === 'priority' ? 'Available' : result.delayMs + ' ms') : 'Unavailable';
  }
  function probeBadge(rule,id) {
   var badge=E('small',{'class':'nk-probe-result','data-probe-id':id}); updateProbeBadge(badge,rule,id);return badge;
  }
  function updateProbeBadge(el,rule,id) {
   var state=ruleView(rule).probe,result=state&&state.results&&state.results[id];
   var error=state&&(state.errors&&state.errors[id]||state.error);
   var status=result?(result.available?'ok':'error'):error?'error':state&&state.pending?'pending':'idle';
   var text=probeText(rule,id);el.setAttribute('data-state',status);el.title=result&&result.reason||error||'';
   domReplace(el,text?[picker.icon({ok:'circle-check',error:'circle-x',pending:'loader-circle'}[status]||'circle-alert'),document.createTextNode(text)]:[]);
  }
  function paintProbe(rule) {
   var index = config.rules.indexOf(rule), state = ruleView(rule).probe;
   if (index<0) return;
   var row = ruleBox.querySelector('[data-rule-index="' + index + '"]');
   if (!row) return;
   row.querySelectorAll('[data-probe-id]').forEach(function(el) { updateProbeBadge(el,rule,el.getAttribute('data-probe-id')); });
   var refresh=row.querySelector('.nk-server-refresh');
   if(refresh){
    refresh.setAttribute('aria-busy',String(!!(state&&state.pending)));
    if(busy&&Object.prototype.hasOwnProperty.call(refresh,'_nadkopDisabled'))refresh._nadkopDisabled=!!(state&&state.pending)||!ruleRefs(rule).length;
    else refresh.disabled=busy||!!(state&&state.pending)||!ruleRefs(rule).length;
   }
   updateServerVisibility(rule);
  }
  function runProbeQueue() {
   if (probeRunning || !probeQueue.length || !root.isConnected) return;
   var rule = probeQueue.shift(), key = probeKey(rule), state = ruleView(rule).probe;
   if (config.rules.indexOf(rule)<0 || rule.action!=='proxy' || !ruleRefs(rule).length) { runProbeQueue(); return; }
   probeRunning = true;
   var mode = (rule.selection || {}).mode || 'priority', url = mode === 'url' ? rule.selection.url : '';
   var refs = ruleRefs(rule).slice(); state.results = {}; state.errors={}; state.mode = mode;
   function requestBatch(refs,attempt) {
    if(probeKey(rule)!==key||ruleView(rule).probe!==state)return Promise.resolve({results:{}});
    return checked(api.probe(refs,mode,url)).then(function(result){
     if(!result.results)throw new Error('No response from the router');return result;
    }).catch(function(err){
     var message=err.message||String(err);
     if(attempt<8&&/Probe already running/.test(message)||attempt<1&&/load servers|timeout|timed out|expired/i.test(message))
      return new Promise(function(resolve){setTimeout(resolve,Math.min(4000,1000*(attempt+1)));}).then(function(){return requestBatch(refs,attempt+1);});
     throw err;
    });
   }
   function batch(offset) {
    if (offset >= refs.length || probeKey(rule)!==key || ruleView(rule).probe !== state) return Promise.resolve();
    var members=refs.slice(offset,offset+4);
    return requestBatch(members,0).then(function(result) {
     if (probeKey(rule)!==key || ruleView(rule).probe !== state) return;
     Object.assign(state.results,result.results || {});
    }).catch(function(err){
     if(probeKey(rule)===key&&ruleView(rule).probe===state)members.forEach(function(id){state.errors[id]=err.message||String(err);});
    }).then(function(){paintProbe(rule);return batch(offset+4);});
   }
   batch(0).then(function() {state.pending=false;}).catch(function(err) {
    if (probeKey(rule)===key && ruleView(rule).probe === state) {state.pending=false;state.error=err.message;}
   }).finally(function() { probeRunning=false;paintProbe(rule);runProbeQueue(); });
  }
  function checkRule(rule, delay) {
   var view = ruleView(rule); clearTimeout(view.probeTimer);
   view.probe = {pending:true}; paintProbe(rule);
   view.probeTimer = setTimeout(function() {
    if (!root.isConnected) return;
    if (probeQueue.indexOf(rule)<0) probeQueue.push(rule); runProbeQueue();
   },delay || 0);
  }
  function actionReason(rule,action) {
   if(action!=='zapret')return '';
   if(!/^[a-zA-Z0-9_.:-]{1,15}$/.test(config.wanInterface||'')||!(config.zapretArgs||[]).length)return 'For Zapret, specify the WAN device and strategy in settings.';
   if(!ruleMatchers(rule).sets||!(rule.sets||[]).length)return '';
   return zapretSetReason(rule.sets);
  }
  function zapretSetReason(sets) {
   if(!geoCatalogue)return 'Loading Zapret compatibility of sets.';
   for(var set of sets||[]) {
    var item=(geoCatalogue.items||[]).find(function(candidate){return candidate.id===set;});
    if(!item)return 'Set "'+set+'" is missing from the loaded database.';
    if(item.unsupported)return set+': '+item.unsupported;
    if(!item.zapret)return '"'+set+'": Xray (VPN) only. For Zapret, turn off "Sets" or choose a compatible set.';
   }
   return '';
  }
  function matcherReason(rule,key) {
   if(key!=='sets'||ruleMatchers(rule).sets||!(rule.sets||[]).length)return '';
   for(var set of rule.sets||[]){var item=(geoCatalogue&&geoCatalogue.items||[]).find(function(candidate){return candidate.id===set;});if(item&&item.unsupported)return set+': '+item.unsupported;}
   return rule.action==='zapret'?zapretSetReason(rule.sets):'';
  }
  function paintActionCompatibility() {
   config.rules.forEach(function(rule,index){
    var section=ruleBox.querySelector('[data-rule-index="'+index+'"]');if(!section)return;
    section.querySelectorAll('[data-rule-action]').forEach(function(control){
     var reason=actionReason(rule,control.getAttribute('data-rule-action'));
     control.title=reason;control.disabled=busy||!!reason;
     if(reason&&rule.action===control.getAttribute('data-rule-action'))control.setAttribute('aria-describedby','nk-action-reason-'+index);else control.removeAttribute('aria-describedby');
    });
    section.querySelectorAll('[data-matcher-key]').forEach(function(control){var reason=matcherReason(rule,control.getAttribute('data-matcher-key'));control.disabled=busy||!!reason;control.title=reason;});
    var note=section.querySelector('.nk-action-reason');if(note) {note.textContent=actionNotice(rule);note.hidden=!note.textContent;}
    var reasonHelp=section.querySelector('.nk-action-help'), reason=actionReason(rule,'zapret');
    if(reasonHelp) {
     var summary=reasonHelp.querySelector('summary');
     reasonHelp.querySelector('p').textContent=reason||'Zapret is available.';
     section.querySelector('#nk-zapret-help-'+index).textContent=reason||'Zapret is available.';
     summary.setAttribute('aria-label',reason?'Why Zapret is unavailable':'Zapret is available');
     summary.tabIndex=reason?0:-1;
     reasonHelp.hidden=!reason&&!reasonHelp.contains(document.activeElement);
    }
   });
  }
  function actionNotice(rule) {return (rule.action==='zapret'?actionReason(rule,'zapret'):'')||matcherReason(rule,'sets');}
  function ruleIssue(rule) {
   var conditions=ruleMatchers(rule);
   if(!['domains','ips','sets'].some(function(key){return conditions[key]&&(rule[key]||[]).length;}))
    return {field:'matchers',message:'No match target specified. Add a domain, IP/CIDR, or set — any single field is enough.'};
   if(rule.action==='proxy'&&!rule.node&&!ruleRefs(rule).length)
    return {field:'action',message:'For the "VPN" action, select a connection with the "Select server" button.'};
   var items=geoCatalogue&&geoCatalogue.items;
   if(items&&conditions.sets)for(var set of rule.sets||[]) {
    var item=items.find(function(candidate){return candidate.id===set;});
    if(item&&item.unsupported)return {field:'sets',message:set+': '+item.unsupported};
    if(item&&rule.action==='zapret'&&item.zapret===false)
     return {field:'sets',message:'Set "'+set+'" is incompatible with Zapret. Choose a different action or remove the set.'};
   }
   var stored=ruleView(rule).validation;
   return stored&&stored.signature===JSON.stringify(rule)?stored:null;
  }
  function paintRuleErrors() {
   if(!ruleBox)return;
   config.rules.forEach(function(rule,index){
    var section=ruleBox.querySelector('[data-rule-index="'+index+'"]');if(!section)return;
    section.querySelectorAll('.nk-rule-error').forEach(function(el){el.remove();});
    section.querySelectorAll('.nk-field-invalid').forEach(function(el){el.classList.remove('nk-field-invalid');});
    section.querySelectorAll('[data-nk-invalid]').forEach(function(el){el.removeAttribute('aria-invalid');el.removeAttribute('aria-describedby');el.removeAttribute('data-nk-invalid');});
    var issue=ruleIssue(rule);if(!issue)return;
    var fields=issue.field==='matchers'?['matchers']:[issue.field],first;
    fields.forEach(function(field){
     var box=section.querySelector('[data-rule-field="'+field+'"]');if(!box)return;
     if(!first)first=box;box.classList.add('nk-field-invalid');
     box.querySelectorAll('input,textarea,select,button').forEach(function(el){el.setAttribute('aria-invalid','true');el.setAttribute('aria-describedby','nk-rule-error-'+index);el.setAttribute('data-nk-invalid','');});
    });
    var error=E('p',{'class':'nk-rule-error',id:'nk-rule-error-'+index,role:'alert'},[issue.message]);
    (first&&first.querySelector('.cbi-value-field')||section.querySelector('.nk-rule-body')).append(error);
   });
  }
  function revealRuleError(index) {
   var rule=config.rules[index];if(!rule)return;
   ruleView(rule).collapsed=false;var issue=ruleIssue(rule);if(issue&&['domains','ips','sets'].indexOf(issue.field)>=0){ruleMatchers(rule)[issue.field]=true;if(ruleView(rule).validation)ruleView(rule).validation.signature=JSON.stringify(rule);}rememberRuleViews();rules();
   var tab=root.querySelector('#nadkop-tab-rules');if(tab)tab.click();
   setTimeout(function(){
    var section=ruleBox.querySelector('[data-rule-index="'+index+'"]');if(!section)return;
    section.scrollIntoView({block:'center'});
    var control=section.querySelector('[aria-invalid=true]');if(control)control.focus({preventScroll:true});
   },0);
  }
  function ruleName(rule, index) { return (rule.name || '').trim() || 'Rule ' + (index + 1); }
  function clearDropState() {
   root.querySelectorAll('.is-over,.is-target').forEach(function(el) { el.classList.remove('is-over', 'is-target'); });
   renderPool();
  }
  function assignServer(id, index) {
   if (busy) return;
   var server = nodeTags.find(function(s) { return s.id === id; });
   if (!server || !server.available || server.separator) return;
   if (index < 0 || !config.rules[index]) {
    notifyError(new Error('First click "Select server" in the target rule or drag the card directly into the rule.')); return;
   }
   var rule = config.rules[index], refs = serversForRule(rule).map(function(s) { return s.id; }).filter(Boolean);
   if (refs.indexOf(id) >= 0) return;
   if (refs.length>=16) { notifyError(new Error('No more than 16 servers per rule')); return; }
   refs.push(id); setRuleRefs(rule,refs);
   ruleView(rule).collapsed = false; rememberRuleViews(); selectedRule = index; changed(); rules(); renderPool(); checkRule(rule);
   if (window.matchMedia('(max-width:950px)').matches) ruleBox.querySelector('[data-rule-index="' + index + '"]').scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
  function chooseRule(index) {
   selectedRule = index; rules(); renderPool();
   if (window.matchMedia('(max-width:950px)').matches) serverPool.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
  function pinned(id) { return config.picker.pinned.indexOf(id) >= 0; }
  function pinServer(id, control) {
   if (busy) return;
   control = document.activeElement === control ? control : null;
   var cards = Array.from(poolList.querySelectorAll('.nk-server-card')), position = cards.findIndex(function(el) { return el.getAttribute('data-server-id') === id; });
   if (pinned(id)) config.picker.pinned = config.picker.pinned.filter(function(ref) { return ref !== id; });
   else {
    config.picker.pinned.push(id);
   }
   changed(); renderPool();
   var remaining = Array.from(poolList.querySelectorAll('.nk-server-card'));
   var card = remaining.find(function(el) { return el.getAttribute('data-server-id') === id; }) || remaining[Math.min(position, remaining.length - 1)];
   restoreActionFocus(control, card || serverPool, '.nk-pin:not(:disabled)', '.nk-picker-search');
  }
  function renderPool() {
   poolTarget.textContent = selectedRule >= 0 && config.rules[selectedRule] ? 'Server for "' + ruleName(config.rules[selectedRule], selectedRule) + '"' : '';
   poolTarget.hidden = !poolTarget.textContent;
   var connections = nodeTags.filter(function(s) { return !s.separator; });
   var labels = new Map(), counts = new Map();
   connections.forEach(function(s) {
    var label=(s.name||s.tag||'Server')+' · '+(sourceTitle(s.source)||'Local nodes');
    labels.set(s.id,label);counts.set(label,(counts.get(label)||0)+1);
   });
   var occurrences = new Map();
   connections.forEach(function(s) {
    var label=labels.get(s.id), number=(occurrences.get(label)||0)+1;occurrences.set(label,number);
    if(counts.get(label)>1)labels.set(s.id,label+' · '+number);
   });
   var active = connections.filter(function(s) { return s.available; }).length;
   poolCount.textContent = active + ' in subscriptions' + (connections.length > active ? ' · ' + (connections.length - active) + ' unavailable' : '');
   var query = poolQuery.toLocaleLowerCase(), sourceOrder = [];
   config.sources.forEach(function(s) { if (sourceOrder.indexOf(s.name) < 0) sourceOrder.push(s.name); });
   nodeTags.forEach(function(s) { if (sourceOrder.indexOf(s.source) < 0) sourceOrder.push(s.source); });
   var visible = nodeTags.filter(function(s) {
    var region = picker.country(s);
    return (!poolSource || s.source === poolSource) && (!onlyPinned || pinned(s.id)) &&
     (!query || ((s.name || s.tag || '') + ' ' + s.protocol + ' ' + (s.transport || '') + ' ' + sourceTitle(s.source) + ' ' + region.label + ' ' + region.name + ' ' + region.city + ' ' + region.code).toLocaleLowerCase().indexOf(query) >= 0);
   }).sort(function(a, b) {
    if (a.source !== b.source) return sourceOrder.indexOf(a.source) - sourceOrder.indexOf(b.source);
    if (a.available !== b.available) return a.available ? -1 : 1;
    return (a.position || 0) - (b.position || 0);
   });
   domReplace(poolList, visible.length ? visible.map(function(s) {
    var usage = config.rules.filter(function(rule) { return rule.action === 'proxy' && serversForRule(rule).some(function(server) { return server.id === s.id; }); }).length;
    var chosen = selectedRule >= 0 && config.rules[selectedRule] ? serversForRule(config.rules[selectedRule]) : [];
    return picker.card(s, { emojiStyle: config.picker.emojiStyle, onDragEnd: clearDropState, draggable: true, hideSource: !!poolSource, sourceTitle:sourceTitle(s.source), isBusy: function() { return busy; },
     accessibleName:labels.get(s.id),
     pinned: pinned(s.id), usage: usage, selected: chosen.some(function(server) { return server.id === s.id; }),
     onSelect: function(id) { assignServer(id, selectedRule); }, onPin: pinServer });
   }) : [E('div', { 'class': 'nk-picker-empty' }, [nodeTags.length ? 'Nothing found' : 'No connections'])]);
  }
  function renderPoolFilters() {
   var names = [];
   nodeTags.forEach(function(s) { if (names.indexOf(s.source) < 0) names.push(s.source); });
   if (poolSource && names.indexOf(poolSource) < 0) poolSource = '';
   var sourceFilter = select(poolSource, [['', 'All subscriptions']].concat(names.sort().map(function(name) { return [name, sourceTitle(name)]; })), function(v) { poolSource = v; renderPool(); });
   sourceFilter.setAttribute('aria-label', 'Subscription filter');
   domReplace(poolFilters, [sourceFilter,
    E('button', { type: 'button', 'class': 'cbi-button ' + (onlyPinned ? 'cbi-button-action' : 'cbi-button-neutral') + ' nk-action' + (onlyPinned ? ' is-active' : ''), 'aria-pressed': onlyPinned ? 'true' : 'false',
     click: function(ev) { var control=document.activeElement===ev.currentTarget?ev.currentTarget:null;onlyPinned = !onlyPinned; renderPoolFilters(); renderPool(); restoreActionFocus(control,poolFilters,'button','button'); } }, [picker.icon('pin'), 'Pinned'])]);
  }
  function fetchNodes() {
   return checked(api.nodes()).then(function(result) {
    nodeTags = result.nodes || []; rules(); renderPoolFilters(); renderPool();
   });
  }
  function geoDisplay(item) {
   if (item.kind==='geoip' && /^[a-z]{2}$/.test(item.name)) {
    try { return new Intl.DisplayNames(['en'],{type:'region'}).of(item.name.toUpperCase()) + ' · ' + item.id; } catch(e) {}
   }
   return item.id;
  }
  function loadGeoCatalogue() {
   return checked(api.geodata()).then(function(result) { geoCatalogue=result;paintActionCompatibility();if(validationAttempted)paintRuleErrors();return result; });
  }
  function openGeoPicker(rule) {
   var query='',kind='',limit=100,content=E('div',{'class':'nk-geo-picker'}),results=E('div',{'class':'nk-geo-results'}),message=E('small',{role:'status'});
   function draw() {
    var items=(geoCatalogue && geoCatalogue.items || []).filter(function(item) {return (!kind||item.kind===kind)&&(!query||(geoDisplay(item)+' '+item.name).toLocaleLowerCase().includes(query));});
    items.sort(function(a,b){var ax=a.id.endsWith(':'+query),bx=b.id.endsWith(':'+query);return ax===bx?a.id.localeCompare(b.id):ax?-1:1;});
    message.textContent=items.length ? 'Showing '+Math.min(limit,items.length)+' of '+items.length : geoCatalogue && geoCatalogue.message || 'Nothing found';
    domReplace(results,items.slice(0,limit).map(function(item) {
     var selected=(rule.sets||[]).indexOf(item.id)>=0,unsupported=!!item.unsupported||rule.action==='zapret'&&!item.zapret;
     return E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-geo-result','aria-pressed':String(selected),disabled:unsupported&&!selected?'':null,
      title:item.unsupported|| (unsupported?'Set is only supported by Xray (VPN)':null),click:function(){
       rule.sets=rule.sets||[];var i=rule.sets.indexOf(item.id);if(i>=0)rule.sets.splice(i,1);else rule.sets.push(item.id);changed();rules();draw();
      }},[E('span',{},[selected?'✓ ':'',geoDisplay(item)]),E('small',{},[item.count + (item.unsupported?' · unavailable':unsupported?' · Xray (VPN) only (VPN)':'')])]);
    }));
    if(items.length>limit)results.append(button('More',function(){limit+=100;draw();}));
   }
   var search=input('',function(value){query=value.trim().toLocaleLowerCase();limit=100;draw();},'search');search.setAttribute('aria-label','Search sets');search.setAttribute('placeholder','github, youtube, telegram, country…');
   var filter=select('',[['','All'],['mixed','Domains + IP'],['geosite','Sites'],['geoip','IP and countries']],function(value){kind=value;draw();});filter.setAttribute('aria-label','Set type');
   content.append(E('div',{'class':'nk-geo-filters'},[search,filter]),message,results);
   ui.showModal('Routing sets',[content,E('div',{'class':'right'},[button('Done',function(){ui.hideModal();})])]);search.focus();
   results.textContent='Loading catalogue…';
   (geoCatalogue?Promise.resolve(geoCatalogue):loadGeoCatalogue()).then(function(){if(content.isConnected)draw();}).catch(function(err){message.textContent=err.message;results.textContent='';});
  }
  function geoRuleField(rule) {
   var box=E('div',{'class':'nk-set-choices'},(rule.sets||[]).map(function(set){return E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-action',title:'Remove set','aria-label':'Remove '+set,
    click:function(){rule.sets=rule.sets.filter(function(value){return value!==set;});changed();rules();}},[set,picker.icon('x')]);}));
   box.append(button('Add set',function(){openGeoPicker(rule);}));return field('Sets',box);
  }
  function ipEditor(rule) {
   var editor=area((rule.ips||[]).join('\n'),function(value){rule.ips=value.split(/[\s,]+/).filter(Boolean);changed();},2);
   function normalize() {var previous=JSON.stringify(rule.ips||[]);rule.ips=editor.value.split(/[\s,]+/).filter(Boolean).map(function(value){try{if(/^https?:\/\//i.test(value))return new URL(value).hostname.replace(/^\[|\]$/g,'');}catch(e){}return value.toLowerCase();});editor.value=rule.ips.join('\n');if(previous!==JSON.stringify(rule.ips))changed();}
   editor.addEventListener('blur',normalize);editor.addEventListener('paste',function(){setTimeout(normalize,0);});editor.setAttribute('placeholder','1.1.1.1, 10.0.0.0/8, 2001:db8::/32');return editor;
  }
  function renderGeoSettings(result) {
   var settings=config.geodata||{},defaults=result.sources||{};
   domReplace(geoSettingsBox,[
    E('p',{role:'status'},[(result.items||[]).length ? 'Sets: '+result.items.length+(result.updatedAt?' · '+new Date(result.updatedAt*1000).toLocaleString():'') : result.message||'Databases not loaded yet']),
    field('Geosite URL',input(settings.geositeURL||defaults.geositeURL||'',function(value){config.geodata=config.geodata||{};config.geodata.geositeURL=value;changed();},'url')),
    field('GeoIP URL',input(settings.geoipURL||defaults.geoipURL||'',function(value){config.geodata=config.geodata||{};config.geodata.geoipURL=value;changed();},'url')),
    button('Update databases',function(){return task(function(){return checked(api.updategeo(config.geodata||{})).then(function(value){geoCatalogue=value;renderGeoSettings(value);});});})
   ]);
  }
  function ruleMatchers(rule) {
   var state=ruleView(rule);
   if(!state.matchers) {
    state.matchers=rule.conditions||{};
    if(!rule.conditions)['domains','ips','sets'].forEach(function(key){state.matchers[key]=!!(rule[key]||[]).length;});
    rule.conditions=state.matchers;
   }
   return state.matchers;
  }
  function matcherSwitches(rule,index) {
   var enabled=ruleMatchers(rule),state=ruleView(rule);
   return field('Condition',E('div',{'class':'nk-matcher-switches'},[['domains','Domains'],['ips','IP / CIDR'],['sets','Sets']].map(function(item){
    var key=item[0],id='nk-matcher-'+index+'-'+key;
    return E('button',{type:'button',id:id,'class':'cbi-button '+(enabled[key]?'cbi-button-action is-active':'cbi-button-neutral')+' nk-action nk-matcher-switch',
     'data-matcher-key':key,disabled:matcherReason(rule,key)?'':null,title:matcherReason(rule,key),'aria-pressed':String(enabled[key]),'aria-controls':'nk-matcher-field-'+index+'-'+key,
     click:function(){
      enabled[key]=!enabled[key];rule.conditions=enabled;
      changed();rules();
      var control=ruleBox.querySelector('#'+id);if(control)control.focus({preventScroll:true});
     }
    },[item[1]]);
   })));
  }

  function matcherField(rule,index,key,control) {
   control.id='nk-matcher-field-'+index+'-'+key;control.hidden=!ruleMatchers(rule)[key];return control;
  }
  function domainEditor(rule) {
   var editor = area((rule.domains || []).join('\n'),function(value) {rule.domains=words(value);changed();},3);
   function normalize() {var previous=JSON.stringify(rule.domains||[]);rule.domains=words(editor.value);editor.value=rule.domains.join('\n');if(previous!==JSON.stringify(rule.domains))changed();}
   editor.addEventListener('blur',normalize);
   editor.addEventListener('paste',function(){setTimeout(normalize,0);});
   return editor;
  }
  function rules() {
   domReplace(ruleBox, config.rules.map(function(r, i) {
    var chosen = serversForRule(r), state = ruleView(r);
    function focusName() {
     var editor = ruleBox.querySelector('[data-rule-index="' + i + '"] .nk-rule-name');
     if (editor) { editor.focus(); editor.select(); }
    }
    function editName() { if(busy)return;state.editing = true; state.draft = r.name || ''; rules(); focusName(); }
    function nameLabel() { return E('span', { 'class':'nk-rule-title', title:ruleName(r,i), dblclick:editName }, [ruleName(r,i)]); }
    function renameButton() { return E('button', { type:'button', 'class':'cbi-button cbi-button-neutral nk-action nk-rule-rename', title:'Rename',
     'aria-label':'Rename ' + ruleName(r,i), click:editName }, [picker.icon('pencil')]); }
    function finishName(commit, focus) {
     if (!state.editing) return;
     state.editing = false;
     if (commit && (r.name || '') !== state.draft.trim()) { r.name = state.draft.trim(); changed(); renderPool(); }
     // Replace only the editor: rerendering the whole rule on blur would
     // remove the button the user is about to click (save, collapse or delete).
     var section = ruleBox.querySelector('[data-rule-index="' + i + '"]');
     if (!section) return;
     var editor = section.querySelector('.nk-rule-name'), rename = renameButton();
     if (editor) editor.replaceWith(nameLabel());
     section.querySelector('.nk-rule-header-actions').prepend(rename);
     section.querySelector('.nk-rule-toggle').setAttribute('aria-label',(state.collapsed ? 'Expand ' : 'Collapse ') + ruleName(r,i));
     section.querySelector('.nk-rule-delete').setAttribute('aria-label','Delete ' + ruleName(r,i));
     lockControls();
     if (focus) rename.focus();
    }
    var name = state.editing ? E('input', { 'class':'cbi-input-text nk-rule-name', type:'text', value:state.draft,
     placeholder:'Rule ' + (i + 1), maxlength:'80', 'aria-label':'Rule name ' + (i + 1),
     input:function(ev) { state.draft = ev.target.value; }, blur:function() { finishName(true); },
     keydown:function(ev) { if (ev.key === 'Enter' || ev.key === 'Escape') { ev.preventDefault(); finishName(ev.key === 'Enter',true); } }
    }) : nameLabel();
    var listToggle=chosen.length>1?E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-action nk-server-list-toggle',
     'aria-label':state.serversCollapsed?'Expand server list':'Collapse server list',title:state.serversCollapsed?'Expand server list':'Collapse server list',
     'aria-expanded':String(!state.serversCollapsed),'aria-controls':'nk-rule-servers-'+i,
     click:function(ev){state.serversCollapsed=!state.serversCollapsed;rememberRuleViews();updateServerVisibility(r);
      var label=state.serversCollapsed?'Expand server list':'Collapse server list';ev.currentTarget.setAttribute('aria-label',label);ev.currentTarget.title=label;ev.currentTarget.setAttribute('aria-expanded',String(!state.serversCollapsed));
      ev.currentTarget.replaceChildren(picker.icon(state.serversCollapsed?'chevron-down':'chevron-up'));
     }},[picker.icon(state.serversCollapsed?'chevron-down':'chevron-up')]):'';
    var drop = r.action === 'proxy' && chosen.length ? E('div', { 'class': 'nk-dropzone nk-rule-server-list', 'data-rule-drop': String(i) }, [
     listToggle, E('ol', { 'class':'nk-rule-servers',id:'nk-rule-servers-'+i,'data-collapsed':String(state.serversCollapsed) }, chosen.map(function(server, memberIndex) {
      return E('li', {hidden:state.serversCollapsed&&memberIndex!==preferredServerIndex(r)?'':null}, [E('span',{'class':'nk-server-position','aria-label':state.serversCollapsed&&chosen.length>1?'Server '+(memberIndex+1)+' of '+chosen.length:'Order '+(memberIndex+1)},[state.serversCollapsed&&chosen.length>1?(memberIndex+1)+' of '+chosen.length:String(memberIndex+1)]),picker.card(server, { emojiStyle:config.picker.emojiStyle, draggable:!!server.id, onDragEnd:clearDropState,
       hideSource:true, trailing:probeBadge(r,server.id), isBusy:function() {return busy;},
       controls:chosen.length>1?[
        E('button', {type:'button','class':'cbi-button cbi-button-neutral nk-move-server','data-move-server':'up',disabled:memberIndex===0 ? '' : null,'aria-label':'Move up server ' + (memberIndex+1),click:function(ev){moveServer(i,memberIndex,memberIndex-1,ev.currentTarget);}}, [picker.icon('arrow-up')]),
        E('button', {type:'button','class':'cbi-button cbi-button-neutral nk-move-server','data-move-server':'down',disabled:memberIndex===chosen.length-1 ? '' : null,'aria-label':'Move down server ' + (memberIndex+1),click:function(ev){moveServer(i,memberIndex,memberIndex+1,ev.currentTarget);}}, [picker.icon('arrow-down')])
       ]:[],
       footer:E('div',{'class':'nk-assigned-footer'},[
        E('small',{'class':'nk-source'},[sourceTitle(server.source)]),
        E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-unlink','aria-label':'Remove server from rule',title:'Remove from rule',click:function(){removeServer(i,memberIndex);}},[picker.icon('x')])
       ]),
       onReorder:function(id,target) { if (!busy) reorderServer(i,id,target); } })
      ]);
     }))
    ]) : '';
    var selection = chosen.length>1 ? field('Server selection', select((r.selection || {}).mode || 'priority', [
     ['priority','By availability: 1 → last'],['ping','Ping test'],['url','URL test']
    ],function(value){r.selection=r.selection||{};r.selection.mode=value;if(value==='url'&&!r.selection.url)r.selection.url='https://connectivitycheck.gstatic.com/generate_204';changed();rules();checkRule(r);}),
     'First available or fastest server. See diagnostics for details.') : '';
    var testURL = chosen.length>1 && r.selection && r.selection.mode==='url' ? field('Check URL',input(r.selection.url || '',function(value){r.selection.url=value;changed();checkRule(r,600);},'url')) : '';
    var addServer = E('button', { type:'button', 'class':'cbi-button cbi-button-neutral nk-action', 'data-choose-rule':String(i),
     click:function() { chooseRule(i); } }, [picker.icon('server'), chosen.length ? 'Add server' : 'Select server']);
    var refreshServers=E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-action nk-server-refresh',
     'aria-label':'Refresh server checks',title:'Refresh server checks','aria-busy':String(!!(state.probe&&state.probe.pending)),
     disabled:!chosen.length||state.probe&&state.probe.pending?'':null,
     click:function(){if(!busy&&chosen.length&&!(state.probe&&state.probe.pending))checkRule(r);}},[picker.icon('refresh-cw')]);
    var serverButtons=E('div',{'class':'nk-server-buttons'},[addServer,refreshServers]);
    var serverToolbar = '';
    if (r.action === 'proxy') {
     if (selection) {
      selection.classList.add('nk-server-toolbar');
      selection.querySelector('.cbi-value-field').append(serverButtons);
      serverToolbar = selection;
     } else serverToolbar = E('div', { 'class':'nk-server-toolbar nk-server-toolbar-single' }, [serverButtons]);
    }

    return E('section', { 'class': 'cbi-section nk-rule', 'data-rule-index': String(i),
     dragover: function(ev) { if (!busy) { picker.allowDrop(ev); if (ev.defaultPrevented) ev.currentTarget.classList.add('is-over'); } },
     dragleave: function(ev) { if (!ev.currentTarget.contains(ev.relatedTarget)) ev.currentTarget.classList.remove('is-over'); },
     drop: function(ev) {
      var id = picker.readDrag(ev); ev.currentTarget.classList.remove('is-over');
      if (id) { ev.preventDefault(); ev.stopPropagation(); assignServer(id, i); clearDropState(); }
     }
    }, [
     E('div', { 'class': 'nk-rule-head' }, [
      E('div', { 'class':'nk-rule-heading' }, [
       E('button', { type:'button', 'class':'cbi-button cbi-button-neutral nk-action nk-rule-toggle',
        'aria-label':(state.collapsed ? 'Expand ' : 'Collapse ') + ruleName(r,i), 'aria-expanded':String(!state.collapsed), 'aria-controls':'nk-rule-body-' + i,
        click:function(ev) { state.collapsed = !state.collapsed; ev.currentTarget.setAttribute('aria-expanded',String(!state.collapsed));
         ev.currentTarget.setAttribute('aria-label',(state.collapsed ? 'Expand ' : 'Collapse ') + ruleName(r,i));
         ev.currentTarget.replaceChildren(picker.icon(state.collapsed ? 'chevron-right' : 'chevron-down'));
         ruleBox.querySelector('#nk-rule-body-' + i).hidden = state.collapsed; rememberRuleViews();
        }
       }, [picker.icon(state.collapsed ? 'chevron-right' : 'chevron-down')]), name
      ]),
      E('div', { 'class':'nk-rule-header-actions' }, [state.editing ? '' : renameButton(),
	  E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-action','data-move-rule':'up',disabled:i===0?'':null,'aria-label':'Move up rule '+(i+1),click:function(ev){moveRule(i,i-1,ev.currentTarget);}},[picker.icon('arrow-up')]),
	  E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-action','data-move-rule':'down',disabled:i===config.rules.length-1?'':null,'aria-label':'Move down rule '+(i+1),click:function(ev){moveRule(i,i+1,ev.currentTarget);}},[picker.icon('arrow-down')]),
      E('button', { type:'button', 'class':'cbi-button cbi-button-neutral nk-action nk-rule-delete', title:'Delete rule', 'aria-label':'Delete ' + ruleName(r,i), click:function() {
       confirmDelete('Delete rule "' + ruleName(r,i) + '"?',function() {
        config.rules.splice(i,1); if (selectedRule === i) selectedRule = -1; else if (selectedRule > i) selectedRule--;
        rememberRuleViews(); changed(); rules(); renderPool();
       });
      } }, [picker.icon('trash-2')])])
     ]),
     E('div', { 'class':'nk-rule-body', id:'nk-rule-body-' + i, hidden:state.collapsed ? '' : null }, [
     matcherSwitches(r,i),
     matcherField(r,i,'domains',field('Domains', domainEditor(r), 'Domains or links, one per line. Subdomains are included automatically.')),
     matcherField(r,i,'ips',field('IP / CIDR',ipEditor(r))),matcherField(r,i,'sets',geoRuleField(r)),
     field('Action', E('div', { 'class': 'nk-rule-actions' }, [['direct', 'Direct'], ['zapret', 'Zapret'], ['proxy', 'VPN'], ['block', 'Block']].map(function(action) {
      var control = E('button', { type: 'button', 'data-rule-action':action[0],disabled:actionReason(r,action[0])?'':null,title:actionReason(r,action[0]), 'class': 'cbi-button ' + (r.action === action[0] ? 'cbi-button-action' : 'cbi-button-neutral') + ' nk-action' + (r.action === action[0] ? ' is-active' : ''),
       'aria-pressed': r.action === action[0] ? 'true' : 'false', click: function() {
        r.action = action[0];
        if(r.action!=='proxy') {if(selectedRule===i)selectedRule=-1;clearTimeout(state.probeTimer);probeQueue=probeQueue.filter(function(rule){return rule!==r;});if(state.probe)state.probe.pending=false;}
        changed(); rules(); renderPool();if(r.action==='proxy'&&ruleRefs(r).length)checkRule(r);
       } }, [picker.icon({ direct: 'arrow-right', zapret: 'shield', proxy: 'server', block: 'shield-ban' }[action[0]]), action[1]]);
      return action[0] !== 'zapret' ? control : E('span',{'class':'nk-zapret-action'},[control,
       E('details',{'class':'nk-action-help',hidden:!actionReason(r,'zapret')?'':null,
        focusout:function(){setTimeout(paintActionCompatibility,0);}},[
        E('summary',{'aria-label':'Why Zapret is unavailable','aria-describedby':'nk-zapret-help-'+i},['?']),
        E('p',{},[actionReason(r,'zapret')])
       ]),E('span',{id:'nk-zapret-help-'+i,hidden:''},[actionReason(r,'zapret')])]);
     }))),
     E('p',{'class':'nk-action-reason',id:'nk-action-reason-'+i,hidden:!actionNotice(r)?'':null},[actionNotice(r)]),
     serverToolbar, testURL, drop
     ])
    ]);
   }));
   paintActionCompatibility();if(validationAttempted)paintRuleErrors();
  }
  var nodeEditor = area(nodesText, function(v) { nodesText = v; changed(); }, 12);
  function moveRule(from,to,control) {
	 if(busy||to<0||to>=config.rules.length)return;
	 control = document.activeElement === control ? control : null;
	 var selected=config.rules[selectedRule],item=config.rules.splice(from,1)[0];
	 config.rules.splice(to,0,item);selectedRule=config.rules.indexOf(selected);
	 rememberRuleViews();changed();rules();renderPool();
	 restoreActionFocus(control,ruleBox.querySelector('[data-rule-index="'+to+'"]'),'[data-move-rule="'+(to<from?'up':'down')+'"]:not(:disabled)','[data-move-rule]:not(:disabled)');
	}
  var importText = '';
  var importEditor = area('', function(v) { importText = v; }, 5);
  var poolSearch = input('', function(v) { poolQuery = v; renderPool(); }, 'search');
  poolSearch.setAttribute('class', 'cbi-input-text nk-picker-search');
  poolSearch.setAttribute('placeholder', 'Server, protocol, transport…');
  poolSearch.setAttribute('aria-label', 'Search connections');
  var serverPool = E('aside', { 'class': 'cbi-section nk-picker', 'aria-label': 'Server picker' }, [
   E('div', { 'class': 'nk-picker-header' }, [
    E('div', { 'class':'nk-picker-title' }, [E('h3', {}, ['Connections']), help('Connections', 'Select a rule and add servers to it. Inside a rule, drag cards to reorder them. One server can be used in multiple rules. The counter shows server availability in subscriptions; internet reachability is checked automatically only for rules with multiple servers.')]),
    poolCount, E('button',{type:'button','class':'cbi-button cbi-button-neutral nk-action nk-server-refresh nk-pool-refresh',
     'aria-label':'Refresh connections',title:'Refresh connections','aria-busy':'false',click:function(ev){
      var control=ev.currentTarget;if(busy)return;control.setAttribute('aria-busy','true');
      return task(fetchNodes).finally(function(){control.setAttribute('aria-busy','false');});
     }},[picker.icon('refresh-cw')])]),
   poolTarget, E('div', { 'class': 'nk-search-field' }, [poolSearch]), poolFilters, poolList
  ]);
  importEditor.setAttribute('placeholder', 'JSON, node links, or base64');
  var tabs = [
   ['rules', 'Rules', [E('p',{'class':'nk-rule-priority'},['When conditions match, the first rule from the top applies.']),E('div', { 'class': 'nk-workspace' }, [
    E('div', { 'class': 'nk-rule-list' }, [ruleBox, button('Add rule', function() {
     config.rules.push({ domains: [], action: 'direct' }); selectedRule = config.rules.length - 1; changed(); rules(); renderPool();
    })]), serverPool
   ])]],
   ['sources', 'Subscriptions', [
    E('section', { 'class': 'cbi-section nk-source-entry' }, [
     field('Link or file', sourceInput, 'HTTPS link or path to JSON on the router starting with /. The subscription is added after pasting or Enter.'),
     sourceMessage,
     E('details', {}, [E('summary', {}, ['Options']),
      field('Send HWID', select('on', [['off', 'Disabled'], ['on', 'Enabled']], function(v) { sourceHWID = v === 'on'; }), 'For HTTPS subscriptions. Turn off before adding if the provider does not use HWID.'),
      field('HTTP headers', area(sourceHeaders, function(v) { sourceHeaders = v; }, 3), 'JSON with headers, e.g. User-Agent or Authorization.')
     ])
    ]), sourceBox
   ]],
   ['settings', 'Settings', [
    settingsSection('Device routing','Choose which home network devices the Nadkop rules apply to.',[
     settingField('Apply rules to the home network',select(config.transparent.enabled?'on':'off',[['off','Disabled'],['on','Enabled']],function(v){networkValue('enabled',v==='on');}), 'Before enabling, disable flow offloading in the OpenWrt firewall settings.'),
     settingField('Home network devices',deviceSelector('lan_devices',config.transparent.interfaces||[],true,function(v){networkValue('interfaces',v);}), 'Select the home network bridge, usually br-lan, plus the guest network if needed. Do not add the internet interface here.'),
     settingField('IPv6 traffic',select(config.transparent.ipv6?'on':'off',[['on','By Nadkop rules'],['off','Block']],function(v){networkValue('ipv6',v==='on');}), 'Blocking applies only to the selected devices and prevents bypassing the rules via IPv6.'),
     settingField('If no rule matches',select(config.default||'direct',[['direct','Direct'],['block','Block']],function(v){config.default=v;changed();}), 'What to do with connections that match no rule.'),
     E('details',{},[E('summary',{},['Exceptions']),
      settingField('Direct addresses',area((config.transparent.bypass||[]).join('\n'),function(v){networkValue('bypass',v.split(/[\s,]+/).filter(Boolean));},3), 'IPs or CIDR subnets, one per line. These addresses bypass the rules. Local and private networks are already excluded.')
     ])
    ]),
    settingsSection('Subscriptions','',[
     settingField('Update every, hours',input(config.updateIntervalHours||0,function(v){config.updateIntervalHours=Number(v);changed();},'number'), '0 — manually; 1 to 168 — automatically.')
    ]),
    settingsSection('Zapret','Used only in rules with the "Zapret" action.',[
     settingField('Internet uplink device',deviceSelector('wan_device',config.wanInterface||'',false,function(v){config.wanInterface=v;changed();paintActionCompatibility();}), 'Select the WAN device the router uses to reach the internet.'),
     settingField('Zapret strategy',zapretEditor, 'nfqws parameters for your provider: one argument per line.'),
     tuneBox
    ]),
    E('details',{'class':'nk-settings-section'},[E('summary',{},['Routing databases']),geoSettingsBox]),
    E('details',{'class':'nk-settings-section nk-network-advanced'},[E('summary',{},['Advanced']),
     E('p',{},['You usually do not need to change these. Internal ports are used on the router; they do not need to be opened to the internet.']),
     settingField('Internal Xray port',input(config.transparent.port||12345,function(v){networkValue('port',Number(v));},'number'), 'Nadkop forwards TCP/UDP traffic of the selected devices here. 12345 by default. Change it if another service occupies the port.'),
     settingField('Internal DNS port',input(config.transparent.dnsPort||1053,function(v){networkValue('dnsPort',Number(v));},'number'), 'Nadkop directs DNS queries of the selected devices here. 1053 by default. Nothing needs to change on clients.'),
     settingField('DNS for regular queries',input(config.transparent.dnsUpstream||'127.0.0.1',function(v){networkValue('dnsUpstream',v);}), 'IP of the DNS server used to resolve real site addresses. 127.0.0.1 is the DNS service of the router itself.'),
     settingField('Local SOCKS port',input(config.socksPort||1080,function(v){config.socksPort=Number(v);changed();},'number'), 'Extra proxy for programs on the router itself: 127.0.0.1:1080 by default. No need to configure it on home network devices.')
    ]),
    E('details',{'class':'nk-settings-section'},[E('summary',{},['Appearance']),
     settingField('Emoji',select(config.picker.emojiStyle||'apple',[['apple','Apple'],['system','System']],function(v){config.picker.emojiStyle=v;changed();rules();renderPool();}), 'Apple is loaded from a CDN; flags are bundled. Colors and styling follow the LuCI theme.')
    ])
   ]],
   ['nodes', 'Import', [
    field('Data', importEditor, 'Import replaces the local list after saving. Add a link subscription on the "Subscriptions" tab.'), button('Import', function() { return task(function() {
     var content = importText.trim();
     if (!content) throw new Error('First paste a subscription link or contents into the field above this button.');
     if (/^https?:\/\//i.test(content) || content.charAt(0) === '/') {
      return insertSource(content).then(function() { importText = ''; importEditor.value = ''; });
     }
     return checked(api.import(content)).then(function(r) {
      nodesText = JSON.stringify(r.nodes, null, 2); nodeEditor.value = nodesText;
      importText = ''; importEditor.value = '';
      if (!config.sources.some(function(s) { return s.file === '/etc/nadkop/nodes.json'; })) {
       var name = 'local'; while (config.sources.some(function(s) { return s.name === name; })) name += '_';
       config.sources.push({ name: name, file: '/etc/nadkop/nodes.json', _headers: '{}' }); sources();
      }
      changed();
     });
    }); }),

    E('details', {}, [E('summary', {}, ['Local nodes']),
     field('JSON', nodeEditor, 'Contains access parameters. Changes apply after saving.')])
   ]],
   ['diagnostics', 'Diagnostics', [
    E('h3', {}, ['Status']), runtimeBox,
    E('h3', {}, ['Saved configuration']),
    E('div', { 'class': 'nk-toolbar' }, [button('Check', function() { return diagnose().then(refresh); })]), diagnosticBox,
    E('h3', {}, ['Service']), serviceControls, serviceInfo
   ]]
  ];
  var tabbar = E('ul', { 'class': 'cbi-tabmenu', role: 'tablist', 'aria-label': 'Nadkop sections' });
  var panes = E('div');
  tabs.forEach(function(t, i) {
   var pane = E('div', { id: 'nadkop-' + t[0], 'class': 'cbi-section', role: 'tabpanel',
    'aria-labelledby': 'nadkop-tab-' + t[0] }, t[2]);
   pane.hidden = !!i; panes.appendChild(pane);
   var link = E('a', { id: 'nadkop-tab-' + t[0], href: '#nadkop-' + t[0], role: 'tab',
    'aria-controls': pane.id, 'aria-selected': i ? 'false' : 'true', tabindex:i?-1:0,
    keydown:function(ev) {
     var links=Array.from(tabbar.querySelectorAll('[role=tab]')),current=links.indexOf(ev.currentTarget),next=current;
     if(ev.key==='ArrowRight')next=(current+1)%links.length;
     else if(ev.key==='ArrowLeft')next=(current+links.length-1)%links.length;
     else if(ev.key==='Home')next=0;
     else if(ev.key==='End')next=links.length-1;
     else return;
     ev.preventDefault();links[next].click();links[next].focus();
    }, click: function(ev) {
     ev.preventDefault();
     Array.prototype.forEach.call(panes.children, function(p) { p.hidden = p !== pane; });
     Array.prototype.forEach.call(tabbar.children, function(item) {
      var active = item === li; item.className = active ? 'cbi-tab' : 'cbi-tab-disabled';
      item.firstChild.setAttribute('aria-selected', active ? 'true' : 'false');
      item.firstChild.tabIndex=active?0:-1;
     });
    } }, [t[1]]);
   var li = E('li', { 'class': i ? 'cbi-tab-disabled' : 'cbi-tab', role: 'presentation' }, [link]);
   tabbar.appendChild(li);
  });
  var root = E('div', { 'class': 'cbi-map nk-app' }, [
   E('link', { rel: 'stylesheet', href: L.resource('nadkop/picker.css') + '?v=0.3.0-r11' }),
   E('div', { 'class': 'nk-heading' }, [E('h2', {}, ['Nadkop']), statusBox]),
   tabbar, panes,
   E('div', { 'class': 'cbi-page-actions' }, [dirty, resetButton, button('Save', function() { return save(false); }),
    button('Save and apply', function() { return save(true); }, true)])
  ]);
  sources(); rules(); renderPoolFilters(); renderPool(); status(data[1]);
  renderGeoSettings({message:"Databases not loaded yet"});
  setTimeout(function(){if(root.isConnected)loadGeoCatalogue().then(renderGeoSettings).catch(function(){});},0);
  function pollStatus() { if (root.isConnected && !busy) return refresh().catch(function() {}); }
  function pollTune() {if(!root.isConnected||tunePending)return;if(!tuneBox.offsetParent&&!tuneActive())return;return refreshTune();}
  poll.add(pollStatus, 5);
  poll.add(pollTune, 2);
  setTimeout(function(){if(root.isConnected)refreshTune();},0);
  return root;
 }
});
