import type { CSSProperties } from "react";

export type AccessAppearance = Readonly<Record<string, CSSProperties>>;

// Access uses a deliberately small visual facade. Loading a full render
// adapter before authentication would enlarge the public attack surface.
export function accessAppearance(_template: string | undefined): AccessAppearance {
  const primary = "#1677ff";
  const radius = 6;
  const font = "Inter, ui-sans-serif, system-ui, sans-serif";
  return Object.freeze({
    canvas:{minHeight:"100vh",display:"grid",placeItems:"center",padding:"clamp(8px, 4vw, 24px)",boxSizing:"border-box",background:"#f5f5f5",color:"rgba(0,0,0,.88)",fontFamily:font},
    card:{width:"100%",maxWidth:420,minWidth:0,boxSizing:"border-box",padding:"clamp(16px, 8vw, 36px)",border:"1px solid #f0f0f0",borderRadius:8,background:"#fff",boxShadow:"0 6px 16px rgba(0,0,0,.08)",overflow:"hidden"},
    header:{minHeight:40,display:"flex",alignItems:"center",gap:10,marginBottom:28,flexWrap:"wrap"}, logo:{width:32,height:32,flex:"0 0 32px",display:"grid",placeItems:"center",borderRadius:6,background:primary,color:"#fff",fontWeight:700}, logoImage:{width:32,height:32,objectFit:"contain"}, locale:{marginLeft:"auto",minHeight:40,maxWidth:"100%",border:"1px solid #d9d9d9",borderRadius:radius,background:"#fff",color:"inherit"},
    title:{margin:0,fontSize:24,lineHeight:1.4}, description:{margin:"8px 0 24px",color:"#86909c",lineHeight:1.6},
    methods:{display:"flex",gap:8,marginBottom:20,overflowX:"auto"}, method:{minHeight:40,padding:"8px 12px",border:"1px solid #d9d9d9",borderRadius:radius,background:"#fff",cursor:"pointer"}, methodActive:{minHeight:40,padding:"8px 12px",border:`1px solid ${primary}`,borderRadius:radius,background:"#e6f4ff",color:primary,cursor:"pointer"},
    form:{display:"grid",gap:16}, field:{display:"grid",gap:7,fontSize:14}, input:{width:"100%",minHeight:40,boxSizing:"border-box",padding:"8px 12px",border:"1px solid #d9d9d9",borderRadius:radius,background:"#fff",color:"inherit",font:"inherit"}, help:{minHeight:18,color:"rgba(0,0,0,.45)"},
    primary:{width:"100%",minWidth:0,minHeight:42,border:0,borderRadius:radius,background:primary,color:"#fff",font:"inherit",cursor:"pointer",whiteSpace:"normal"}, secondary:{minWidth:0,minHeight:40,padding:"0 16px",border:"1px solid #d9d9d9",borderRadius:radius,background:"#fff",font:"inherit",cursor:"pointer",whiteSpace:"normal"}, actions:{display:"flex",gap:10,flexWrap:"wrap"},
    error:{padding:"10px 12px",borderRadius:radius,background:"#fff2f0",color:"#cb2634",fontSize:14}, footer:{minHeight:24,display:"flex",justifyContent:"center",gap:20,marginTop:24,fontSize:13,flexWrap:"wrap"},
  });
}
