import type { CSSProperties } from "react";
import type { AccessLocaleSelectorStyles } from "./access-locale-selector";
import type { AccessMethodSelectorStyles } from "./access-method-selector";
import type { AccessSystemScheme } from "./access-system-scheme";

export type AccessAppearance = Readonly<Record<string, CSSProperties>> & AccessLocaleSelectorStyles & AccessMethodSelectorStyles;

// Access uses a deliberately small visual facade. Loading a full render
// adapter before authentication would enlarge the public attack surface.
export function accessAppearance(_template: string | undefined, scheme: AccessSystemScheme = "light"): AccessAppearance {
  const primary = "#1677ff";
  const radius = 6;
  const font = "Inter, ui-sans-serif, system-ui, sans-serif";
  const colors = scheme === "dark"
    ? { canvas: "#141414", surface: "#1f1f1f", raisedSurface: "#262626", text: "rgba(255,255,255,.85)", muted: "rgba(255,255,255,.45)", border: "#434343", active: "rgba(22,119,255,.22)", errorSurface: "#2a1215", errorText: "#ff7875", shadow: "0 6px 16px rgba(0,0,0,.36)" }
    : { canvas: "#f5f5f5", surface: "#fff", raisedSurface: "rgba(255,255,255,.9)", text: "rgba(0,0,0,.88)", muted: "rgba(0,0,0,.45)", border: "#d9d9d9", active: "#e6f4ff", errorSurface: "#fff2f0", errorText: "#cb2634", shadow: "0 6px 16px rgba(0,0,0,.08)" };
  return Object.freeze({
    canvas:{minHeight:"100vh",position:"relative",display:"grid",placeItems:"center",padding:"clamp(8px, 4vw, 24px)",boxSizing:"border-box",background:colors.canvas,color:colors.text,colorScheme:scheme,fontFamily:font},
    card:{width:"100%",maxWidth:420,minWidth:0,boxSizing:"border-box",padding:"clamp(16px, 8vw, 36px)",border:`1px solid ${colors.border}`,borderRadius:8,background:colors.surface,boxShadow:colors.shadow,overflow:"hidden"},
    header:{minHeight:40,display:"flex",alignItems:"center",gap:10,marginBottom:28,flexWrap:"wrap"}, logo:{width:32,height:32,flex:"0 0 32px",display:"grid",placeItems:"center",borderRadius:6,background:primary,color:"#fff",fontWeight:700}, logoImage:{width:32,height:32,objectFit:"contain"},
    localePicker:{position:"absolute",top:"clamp(12px, 2vw, 24px)",right:"clamp(12px, 2vw, 24px)",zIndex:1}, localeTrigger:{width:32,height:32,display:"grid",placeItems:"center",padding:0,border:`1px solid ${colors.border}`,borderRadius:radius,background:colors.raisedSurface,color:"inherit",font:"inherit",cursor:"pointer"}, localeGlyph:{fontSize:12,lineHeight:1,fontWeight:650}, localeMenu:{position:"absolute",top:"calc(100% + 6px)",right:0,minWidth:128,display:"grid",gap:2,padding:4,border:`1px solid ${colors.border}`,borderRadius:radius,background:colors.surface,boxShadow:colors.shadow}, localeOption:{minHeight:30,display:"flex",alignItems:"center",gap:8,padding:"0 8px",border:0,borderRadius:4,background:"transparent",color:"inherit",font:"inherit",fontSize:13,textAlign:"left",cursor:"pointer"}, localeOptionActive:{minHeight:30,display:"flex",alignItems:"center",gap:8,padding:"0 8px",border:0,borderRadius:4,background:colors.active,color:primary,font:"inherit",fontSize:13,textAlign:"left",cursor:"pointer"}, localeOptionGlyph:{width:22,display:"inline-grid",placeItems:"center",fontSize:12,lineHeight:1,fontWeight:650},
    title:{margin:0,fontSize:24,lineHeight:1.4}, description:{margin:"8px 0 24px",color:colors.muted,lineHeight:1.6}, connecting:{margin:"8px 0 24px",color:colors.muted,lineHeight:1.6},
    methods:{display:"flex",gap:8,marginBottom:20,overflowX:"auto"}, method:{minHeight:40,padding:"8px 12px",border:`1px solid ${colors.border}`,borderRadius:radius,background:colors.surface,color:colors.text,cursor:"pointer"}, methodActive:{minHeight:40,padding:"8px 12px",border:`1px solid ${primary}`,borderRadius:radius,background:colors.active,color:primary,cursor:"pointer"}, methodSelect:{display:"grid",gap:7,marginBottom:20}, methodSelectLabel:{fontSize:14,fontWeight:600}, methodSelectInput:{width:"100%",minHeight:40,boxSizing:"border-box",padding:"8px 12px",border:`1px solid ${colors.border}`,borderRadius:radius,background:colors.surface,color:"inherit",font:"inherit"},
    form:{display:"grid",gap:16}, field:{display:"grid",gap:7,fontSize:14}, input:{width:"100%",minHeight:40,boxSizing:"border-box",padding:"8px 12px",border:`1px solid ${colors.border}`,borderRadius:radius,background:colors.surface,color:"inherit",font:"inherit"}, help:{minHeight:18,color:colors.muted},
    primary:{width:"100%",minWidth:0,minHeight:42,border:0,borderRadius:radius,background:primary,color:"#fff",font:"inherit",cursor:"pointer",whiteSpace:"normal"}, secondary:{minWidth:0,minHeight:40,padding:"0 16px",border:`1px solid ${colors.border}`,borderRadius:radius,background:colors.surface,color:colors.text,font:"inherit",cursor:"pointer",whiteSpace:"normal"}, actions:{display:"flex",gap:10,flexWrap:"wrap"},
    error:{padding:"10px 12px",borderRadius:radius,background:colors.errorSurface,color:colors.errorText,fontSize:14}, footer:{minHeight:24,display:"flex",justifyContent:"center",gap:20,marginTop:24,fontSize:13,flexWrap:"wrap"},
  });
}
