// Lightweight form adapter for editor tests; the real DeviceSelect is verified on LuCI.
export function installDeviceWidgets() {
 window.widgets={DeviceSelect:class {
  renderWidget(section,index,value) {
   const select=document.createElement('select');select.multiple=this.multiple;select.id='device-'+this.key;
   const values=this.multiple?value:[value];
   for(const name of [...new Set(['','br-lan','guest','eth0','eth1',...values])]) {
    const option=document.createElement('option');option.value=name;option.textContent=name||'Not specified';option.selected=values.includes(name);select.append(option);
   }
   select.addEventListener('change',()=>{
    const values=[...select.selectedOptions].filter(o=>o.value).map(o=>({value:o.value,text:o.textContent,element:o}));
    const instance={getValue:()=>this.multiple?values.map(o=>o.value):values[0]?.value};
    select.dispatchEvent(new CustomEvent('cbi-dropdown-change',{detail:{instance,value:values[0]||null,values}}));
   });
   return select;
  }
 }};
 window.form={NamedSection:class{},JSONMap:class {
  section(){return {option(Widget,key){const option=new Widget();option.key=key;return option;}};}
 }};
}
