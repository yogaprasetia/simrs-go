// autocomplete sederhana: <input data-ac="pasien|penyakit|icd9|obat">
(function(){
const P=window.P||"";
function esc(s){return String(s??"").replace(/[&<>"]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"}[c]))}
document.querySelectorAll("input[data-ac]").forEach(inp=>{
  const kind=inp.dataset.ac;let box=null,t=null,items=[],sel=-1;
  const close=()=>{box&&box.remove();box=null;sel=-1};
  const pick=i=>{const it=items[i];if(!it)return;
    if(kind==="pasien"){inp.value=it.no_rkm_medis+" — "+it.nm_pasien;const h=document.getElementById("no_rkm_medis");h&&(h.value=it.no_rkm_medis);
      const pi=document.getElementById("pinfo");pi&&(pi.textContent=it.jk+" · "+it.tgl_lahir+" · "+it.alamat+(it.no_peserta?" · BPJS "+it.no_peserta:""));
      const pj=document.getElementById("kd_pj");pj&&it.kd_pj&&(pj.value=it.kd_pj);
      const f=inp.form;if(f){const n=f.querySelector("[name=p_jawab]");}
    }else{inp.value=it.kode+" "+it.nama}
    close();};
  inp.addEventListener("input",()=>{clearTimeout(t);const q=inp.value.trim();if(kind==="pasien"){const h=document.getElementById("no_rkm_medis");h&&(h.value="")}
    if(q.length<2){close();return}
    t=setTimeout(async()=>{const r=await fetch(P+"/api/"+kind+"?q="+encodeURIComponent(q));if(!r.ok)return;items=await r.json()||[];
      close();if(!items.length)return;box=document.createElement("div");box.className="ac";
      box.innerHTML=items.map((it,i)=>kind==="pasien"?`<div data-i="${i}"><b>${esc(it.no_rkm_medis)}</b> ${esc(it.nm_pasien)} <small>${esc(it.jk)} · ${esc(it.tgl_lahir)} · ${esc(it.alamat)}</small></div>`:`<div data-i="${i}"><b>${esc(it.kode)}</b> ${esc(it.nama)}</div>`).join("");
      const r2=inp.getBoundingClientRect();box.style.left=(r2.left+scrollX)+"px";box.style.top=(r2.bottom+scrollY+2)+"px";box.style.width=Math.max(r2.width,320)+"px";
      box.addEventListener("mousedown",e=>{const d=e.target.closest("[data-i]");d&&(e.preventDefault(),pick(+d.dataset.i))});document.body.appendChild(box);},220)});
  inp.addEventListener("keydown",e=>{if(!box)return;const ds=box.querySelectorAll("[data-i]");
    if(e.key==="ArrowDown"||e.key==="ArrowUp"){e.preventDefault();sel=(sel+(e.key==="ArrowDown"?1:-1)+ds.length)%ds.length;ds.forEach((d,i)=>d.classList.toggle("sel",i===sel))}
    else if(e.key==="Enter"){e.preventDefault();pick(sel<0?0:sel)}else if(e.key==="Escape")close()});
  inp.addEventListener("blur",()=>setTimeout(close,150));
});
// registrasi: dokter mengikuti jadwal poli hari ini
const poli=document.getElementById("kd_poli"),dok=document.getElementById("kd_dokter");
if(poli&&dok){poli.addEventListener("change",async()=>{dok.innerHTML="<option>memuat…</option>";const r=await fetch(P+"/api/dokter?poli="+encodeURIComponent(poli.value));const l=await r.json()||[];
  dok.innerHTML=l.map(d=>`<option value="${esc(d.kd_dokter)}">${esc(d.nm_dokter)}${d.jam?" · "+esc(d.jam)+" · "+esc(d.terdaftar)+"/"+esc(d.kuota):""}</option>`).join("")||"<option value=''>tidak ada dokter</option>"});}
const pj=document.getElementById("kd_pj");if(pj&&window.PJ_DEFAULT)pj.value=window.PJ_DEFAULT;
const rf=document.getElementById("regform");rf&&rf.addEventListener("submit",e=>{if(!document.getElementById("no_rkm_medis").value){e.preventDefault();alert("Pilih pasien dari daftar pencarian dulu")}});
// flag H/L langsung saat input hasil lab
function flag(v,r){v=parseFloat(String(v).replace(",","."));if(isNaN(v))return"";r=String(r).replace(/,/g,".").trim();
  if(r[0]=="<")return v>=parseFloat(r.slice(1))?"H":"";if(r[0]==">")return v<=parseFloat(r.slice(1))?"L":"";
  const m=r.split("-");if(m.length==2){const lo=parseFloat(m[0]),hi=parseFloat(m[1]);if(!isNaN(lo)&&!isNaN(hi))return v<lo?"L":v>hi?"H":""}return""}
document.querySelectorAll("input.hasil").forEach(i=>i.addEventListener("input",()=>{const f=flag(i.value,i.dataset.ruj);i.className="hasil "+f;const td=i.closest("tr").querySelector(".flag");td&&(td.textContent=f)}));
})();