import math,random,html,json,re
random.seed(41)
p='canvas/project/App9.dc.html'
s=open('canvas/project/App8.dc.html').read()   # always rebuild from K
CW,LH=6.6,14
PW,PH=1156,830
W,HH=int(PW/CW)+1,60
grid=[[(' ',None) for _ in range(W)] for _ in range(HH)]
def put(c,r,ch,col):
    if 0<=c<W and 0<=r<HH: grid[r][c]=(ch,col)
def put_text(c,r,t,col):
    for i,ch in enumerate(t): put(c+i,r,ch,col)
g=lambda x,y:(int(round(x/CW)),int(round((y-7)/LH)))
WOOD='#c58a62'; KNOT='#a86f4b'
FOL=['&','%','@','#','8','o','*','&','%','@','&']
EDGE=[',',';','"','`',"'",'^','~']
CAN=['#4ade80']*5+['#6bfb9a']*2+['#2fa35c']*3
def blob(x,y,rx,ry,fill=0.78):
    ccx,ccy=x/CW,(y-7)/LH; rxc,ryc=rx/CW,ry/LH
    for r in range(int(ccy-ryc-2),int(ccy+ryc+3)):
        for c in range(int(ccx-rxc-2),int(ccx+rxc+3)):
            dx=(c-ccx)/rxc; dy=(r-ccy)/ryc
            d=dx*dx+dy*dy+(random.random()-0.5)*0.4
            if d<fill:
                if random.random()<0.05: continue
                put(c,r,random.choice(FOL),random.choice(CAN))
            elif d<1.0:
                put(c,r,random.choice(EDGE+FOL[:3]),random.choice(CAN))
# ---------- bonsai: tucked into the bottom-left corner, translucent ----------
TX=99
cb=int(round(TX/CW)); top=49; bot=54
def center(r): return cb+int(round(3*math.sin((r-top)*0.55)*min(1,(bot-3-r)/3)))
for r in range(top,bot+1):
    c0=center(r); w=5
    if r==bot: w=13
    elif r==bot-1: w=9
    elif r==bot-2: w=7
    h=w//2
    nxt=center(r+1) if r<bot else c0
    sh=c0-nxt
    edge='/' if sh>0 else ('\\' if sh<0 else '|')
    for c in range(c0-h,c0+h+1): put(c,r,'|',WOOD)
    if r>=bot-2:
        put(c0-h,r,'/',WOOD); put(c0+h,r,'\\',WOOD)
        if r==bot:
            for k in (1,2,3): put(c0-h-k,r,'_',WOOD); put(c0+h+k,r,'_',WOOD)
    else:
        put(c0-h,r,edge,WOOD); put(c0+h,r,edge,WOOD); put(c0-h+1,r,edge,WOOD); put(c0+h-1,r,edge,WOOD)
for r,off in ((50,0),(52,1)): put(center(r)+off,r,'@',KNOT)
for x,y,rx,ry in [(TX,656,58,14),(TX,682,46,12),(TX-42,676,30,11),(TX+42,676,30,11)]:
    blob(x,y,rx,ry)
content='(@) [ password-keeper ]'
L=len(content); c0=7
put_text(c0+2,55,'.'+'-'*(L-6)+'.',WOOD)
put_text(c0,56,content,'#f5ded4')
put_text(c0,56,'(@)',KNOT)
put_text(c0+2,57,"'"+'-'*(L-6)+"'",WOOD)
log_right=(c0+L)*CW
esc=lambda t: html.escape(t,quote=False)
lines=[]
for row in grid:
    out=[];cur=None;buf=''
    def flush():
        global buf
        if buf: out.append(f'<span style="color: {cur};">{esc(buf)}</span>' if cur else esc(buf))
        buf=''
    for ch,col in row:
        if col!=cur: flush(); cur=col
        buf+=ch
    flush()
    lines.append(''.join(out).rstrip())
pre=f'<pre class="mono" aria-hidden="true" style="position: absolute; left: 0; top: 0; margin: 0; width: {PW}px; font-size: 11px; line-height: 14px; letter-spacing: 0; white-space: pre; opacity: 0.62; pointer-events: none; user-select: none;">'+'\n'.join(lines)+'</pre>'
# ---------- icons / small pieces ----------
def svg(w,d,stroke='currentColor',sw='2',fill='none'):
    return f'<svg width="{w}" height="{w}" viewBox="0 0 24 24" fill="{fill}" stroke="{stroke}" stroke-width="{sw}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">{d}</svg>'
PRI='<circle cx="6" cy="5" r="2"></circle><circle cx="6" cy="19" r="2"></circle><circle cx="18" cy="19" r="2"></circle><path d="M6 7v10"></path><path d="M18 17V11a3 3 0 0 0-3-3h-3"></path>'
BRI='<circle cx="6" cy="5" r="2"></circle><circle cx="6" cy="19" r="2"></circle><circle cx="18" cy="8" r="2"></circle><path d="M6 7v10"></path><path d="M18 10c0 4-6 3-11.5 7.5"></path>'
OK='<circle cx="12" cy="12" r="9"></circle><path d="M8 12l3 3 5-6"></path>'
BAD='<circle cx="12" cy="12" r="9"></circle><path d="M9 9l6 6M15 9l-6 6"></path>'
SPIN='<circle cx="12" cy="12" r="9" opacity="0.3"></circle><path d="M12 3a9 9 0 0 1 9 9"></path>'
I_OPEN='<path d="M14 4h6v6"></path><path d="M20 4l-9 9"></path><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"></path>'
I_STOP='<rect x="6" y="6" width="12" height="12" rx="2"></rect>'
I_RESTART='<path d="M20 11a8 8 0 1 0-2.3 5.7"></path><path d="M20 4v7h-7"></path>'
I_TERM='<path d="M4 6l6 6-6 6"></path><path d="M12 19h8"></path>'
I_PAUSE='<path d="M9 6v12M15 6v12"></path>'
I_PLAY='<path d="M8 5l11 7-11 7z"></path>'
I_REPLY='<path d="M9 14l-5-5 5-5"></path><path d="M4 9h11a5 5 0 0 1 5 5v5"></path>'
I_PLUS='<path d="M12 5v14M5 12h14"></path>'
I_PULL='<path d="M12 4v12"></path><path d="M7 11l5 5 5-5"></path><path d="M5 20h14"></path>'
CI={'ok':(OK,'#a4f0cd','rgba(164, 240, 205, 0.1)','CI/CD passing'),'bad':(BAD,'#ffb4ab','rgba(147, 0, 10, 0.28)','CI/CD failing'),'run':(SPIN,'#6bfb9a','rgba(74, 222, 128, 0.12)','CI/CD running')}
def cibadge(k,label=False):
    ic,fg,bg,t=CI[k]
    anim=' animation: bspin 1.2s linear infinite;' if k=='run' else ''
    txt='<span class="mono" style="font-size: 10.5px;">CI/CD</span>' if label else ''
    w='auto' if label else '22px'; pad='0 6px' if label else '0'
    return f'<span title="{t}" style="min-width: 22px; width: {w}; height: 20px; padding: {pad}; box-sizing: border-box; flex: none; display: inline-flex; align-items: center; justify-content: center; gap: 4px; border-radius: 5px; background: {bg}; color: {fg};"><span style="display: inline-flex;{anim}">{svg(12,ic)}</span>{txt}</span>'
def prbadge(n,st):
    fg,bg={'draft':('#bccabb','#342721'),'appr':('#a4f0cd','rgba(164, 240, 205, 0.1)'),'open':('#6bfb9a','rgba(74, 222, 128, 0.12)')}[st]
    return f'<span class="mono" title="PR #{n} · {st}" style="flex: none; display: inline-flex; align-items: center; gap: 4px; height: 20px; padding: 0 6px; border-radius: 5px; background: {bg}; color: {fg}; font-size: 10.5px;">{svg(12,PRI)}#{n}</span>'
def btn(icon,label,hot=False):
    bd='rgba(217, 119, 70, 0.55)' if hot else '#3d271d'
    fg='#ffb694' if hot else '#bccabb'
    bg='rgba(217, 119, 70, 0.14)' if hot else '#2b1d16'
    return f'<button type="button" class="ghost" aria-label="{label}" title="{label}" style="width: 20px; height: 20px; padding: 0; flex: none; display: grid; place-items: center; border: 1px solid {bd}; border-radius: 6px; background: {bg}; color: {fg};">{svg(11,icon,sw="2")}</button>'
# ---------- node geometry ----------
CWD,CHT=312,66            # worktree card
SW_,SH_,SG=153,54,6       # satellite (process / agent) node
LANE=[30,422,814]
TIER={1:484,2:238,3:52}
MAIN=dict(x=393,y=742,w=370,h=86)
# sats: ('p',name,port,meta,state) | ('a',agent,task,state)
WT={
 'pk':dict(name='feat/passkeys',lane=0,t=1,base='main',ci='run',pr=34,prs='open',diff='+64 −3',ab=(3,0),
      sats=[('p','dev',':3002','42m · 118MB','run'),('a','claude','wiring WebAuthn','work')]),
 'rv':dict(name='feat/review',lane=0,t=2,base='pk',ci='run',pr=26,prs='open',diff='+8 −2',ab=(2,1),sel=True,
      sats=[('p','dev',':5173','12m · 96MB','run'),('a','claude','reviewing diff','work')]),
 'ui':dict(name='feat/passkey-ui',lane=0,t=3,base='rv',ci='ok',pr=37,prs='draft',diff='+120 −14',ab=(5,0),
      sats=[('p','storybook',':6006','1h · 240MB','run'),('a','antigravity','idle · 8m','idle')]),
 'ri':dict(name='feat/react-init-time',lane=1,t=1,base='main',ci='bad',pr=31,prs='draft',diff='+142 −18',ab=(7,4),
      sats=[('a','antigravity','profiling boot','work')]),
 'lz':dict(name='perf/lazy-routes',lane=1,t=2,base='ri',ci='ok',pr=39,prs='draft',diff='+37 −9',ab=(1,0),sats=[]),
 'se':dict(name='fix/session-expiry',lane=2,t=1,base='main',ci='ok',pr=29,prs='appr',diff='+31 −4',ab=(2,0),
      sats=[('p','api',':8080','2h · 64MB','run'),('p','worker',':9100','stopped','idle'),('a','claude','idle · 4m','idle'),('a','codex','running tests','work')]),
 'ts':dict(name='refactor/token-store',lane=2,t=2,base='se',ci='bad',pr=28,prs='draft',diff='+210 −96',ab=(6,2),
      sats=[('p','test','','exit 1 · 2m ago','fail'),('a','claude','fixing 3 tests','work'),('a','codex','needs reply','wait')]),
 'tr':dict(name='feat/token-rotation',lane=2,t=3,base='ts',ci='run',pr=38,prs='draft',diff='+48 −2',ab=(2,0),
      sats=[('p','tsc -w','','watching · 61MB','run'),('a','antigravity','idle · 12m','idle')]),
}
NAME=lambda k:'rust-backend' if k=='main' else WT[k]['name']
for k,w in WT.items():
    w['x']=LANE[w['lane']]; w['y']=TIER[w['t']]
    n=len(w['sats'])
    w['h']=CHT+10+(30 if n==0 else (SH_ if n<=2 else SH_*2+SG))
    w['bot']=w['y']+w['h']
SEL={'rv','pk'}   # selected path: feat/review -> feat/passkeys -> default
AG={'claude':('CL','Claude','rgba(217, 119, 70, 0.22)','#ffb694'),
    'antigravity':('AG','Antigravity','rgba(164, 240, 205, 0.14)','#a4f0cd'),
    'codex':('CX','Codex','#3f322b','#f5ded4')}
def logo(a):
    ab,_,bg,fg=AG[a]
    return f'<span class="mono" style="flex: none; width: 18px; height: 18px; display: grid; place-items: center; border-radius: 5px; background: {bg}; color: {fg}; font-size: 8.5px; font-weight: 700; letter-spacing: 0;">{ab}</span>'
def proc_node(x,y,sp):
    _,name,port,meta,st=sp
    dot={'run':'#4ade80','fail':'#ffb694','idle':'#869486'}[st]
    bd='rgba(255, 182, 148, 0.4)' if st=='fail' else '#342721'
    mc='#ffb694' if st=='fail' else '#869486'
    b={'run':btn(I_OPEN,'Open')+btn(I_STOP,'Stop'),'fail':btn(I_TERM,'Logs')+btn(I_RESTART,'Restart',True),'idle':btn(I_TERM,'Logs')+btn(I_PLAY,'Start')}[st]
    pt=f'<span class="mono" style="margin-left: auto; font-size: 10px; color: #a4f0cd;">{port}</span>' if port else ''
    return f'''<div style="position: absolute; left: {x}px; top: {y}px; width: {SW_}px; height: {SH_}px; box-sizing: border-box; padding: 6px 8px; border-radius: 10px; background: #1c110b; border: 1px solid {bd};">
<div style="display: flex; align-items: center; gap: 6px; height: 20px;"><span style="width: 7px; height: 7px; flex: none; border-radius: 2px; background: {dot};"></span><span class="mono" style="font-size: 11px; font-weight: 600; color: #f5ded4; white-space: nowrap;">{name}</span>{pt}</div>
<div style="display: flex; align-items: center; justify-content: space-between; gap: 4px; height: 20px; margin-top: 2px;"><span class="mono" style="min-width: 0; font-size: 9.5px; color: {mc}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">{meta}</span><span style="display: inline-flex; gap: 2px; flex: none;">{b}</span></div>
</div>'''
def agent_node(x,y,sp):
    _,a,task,st=sp
    dot={'work':'#4ade80','wait':'#d97746','idle':'#869486'}[st]
    glow={'work':'rgba(74, 222, 128, 0.2)','wait':'rgba(217, 119, 70, 0.25)','idle':'rgba(134, 148, 134, 0.15)'}[st]
    tc={'work':'#bccabb','wait':'#ffb694','idle':'#869486'}[st]
    bd='rgba(217, 119, 70, 0.55)' if st=='wait' else '#4a3328'
    b=btn(I_TERM,'Open terminal',st=='wait')+btn({'work':I_PAUSE,'idle':I_PLAY,'wait':I_REPLY}[st],{'work':'Pause','idle':'Resume','wait':'Reply'}[st])
    return f'''<div style="position: absolute; left: {x}px; top: {y}px; width: {SW_}px; height: {SH_}px; box-sizing: border-box; padding: 6px 8px; border-radius: 10px; background: #2b1d16; border: 1px solid {bd};">
<div style="display: flex; align-items: center; gap: 6px; height: 20px;">{logo(a)}<span style="font-size: 11.5px; font-weight: 600; color: #f5ded4; white-space: nowrap;">{AG[a][1]}</span><span title="{st}" style="margin-left: auto; width: 6px; height: 6px; flex: none; border-radius: 9999px; background: {dot}; box-shadow: 0 0 0 3px {glow};"></span></div>
<div style="display: flex; align-items: center; justify-content: space-between; gap: 4px; height: 20px; margin-top: 2px;"><span class="mono" style="min-width: 0; font-size: 9.5px; color: {tc}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">{task}</span><span style="display: inline-flex; gap: 2px; flex: none;">{b}</span></div>
</div>'''
def ghost_node(x,y,label,h=SH_):
    return f'<div class="mono" style="position: absolute; left: {x}px; top: {y}px; width: {SW_}px; height: {h}px; box-sizing: border-box; display: flex; align-items: center; justify-content: center; gap: 5px; border-radius: 10px; border: 1px dashed #3d271d; color: #6b5348; font-size: 10px;">{svg(10,I_PLUS)}{label}</div>'
def card(k):
    w=WT[k]; sel=w.get('sel',False)
    bd='rgba(74, 222, 128, 0.55)' if sel else '#3d271d'
    sh='0 0 0 3px rgba(74, 222, 128, 0.10), ' if sel else ''
    d=w['diff'].replace('+','<span style="color: #6bfb9a;">+').replace(' −','</span> <span style="color: #ffb694;">−')+'</span>'
    x,y=w['x'],w['y']
    a,b=w['ab']
    abh=f'<span class="mono" title="ahead / behind {NAME(w["base"])}" style="flex: none; font-size: 10px; color: #869486; white-space: nowrap;">↑{a} <span style="color: {"#ffb694" if b else "#869486"};">↓{b}</span></span>'
    bn=NAME(w['base']); bc='#869486' if w['base']=='main' else '#a4f0cd'
    out=f'''<article style="position: absolute; left: {x}px; top: {y}px; width: {CWD}px; height: {CHT}px; box-sizing: border-box; padding: 8px 12px; border-radius: 12px; background: #251913; border: 1px solid {bd}; box-shadow: {sh}inset 0 1px 0 0 rgba(245, 222, 212, 0.04), 0 14px 28px -16px rgba(10, 7, 5, 0.85);">
<div style="display: flex; align-items: center; justify-content: space-between; gap: 8px; height: 20px;"><span class="mono" style="min-width: 0; display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 600; color: #f5ded4; white-space: nowrap;"><span style="flex: none; display: inline-flex; color: {'#6bfb9a' if sel else '#869486'};">{svg(12,BRI)}</span><span style="overflow: hidden; text-overflow: ellipsis;">{w['name']}</span></span><span class="mono" style="flex: none; font-size: 10.5px; color: #869486; white-space: nowrap;">{d}</span></div>
<div style="display: flex; align-items: center; gap: 5px; height: 20px; margin-top: 6px;">{cibadge(w['ci'])}{prbadge(w['pr'],w['prs'])}{abh}<span class="mono" style="min-width: 0; margin-left: auto; font-size: 10px; color: {bc}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">→ {bn}</span></div>
</article>
'''
    sats=w['sats']; n=len(sats)
    ncell=0 if n==0 else (2 if n<=2 else 4)
    if n==0:
        sy=y+CHT+10
        out+=ghost_node(x,sy,'process',30)+ghost_node(x+SW_+SG,sy,'agent',30)+'\n'
        return out
    for i in range(ncell):
        r,c=divmod(i,2)
        sx=x+c*(SW_+SG); sy=y+CHT+10+r*(SH_+SG)
        if i<n:
            sp=sats[i]
            col='rgba(74, 222, 128, 0.55)' if sp[0]=='p' else 'rgba(164, 240, 205, 0.5)'
            top_=y+CHT if r==0 else sy-SG
            hh=10 if r==0 else SG
            out+=f'<span style="position: absolute; left: {sx+SW_//2}px; top: {top_}px; width: 1px; height: {hh}px; background: {col};"></span>\n'
            out+=(proc_node(sx,sy,sp) if sp[0]=='p' else agent_node(sx,sy,sp))+'\n'
        else:
            kinds=[q[0] for q in sats]
            lab='process' if kinds.count('a')>=kinds.count('p') else 'agent'
            out+=ghost_node(sx,sy,lab)+'\n'
    return out
cards='\n'.join(card(k) for k in WT)
# ---------- links ----------
links=[];chips=[]
def chip(x,y,label,col,bg,bd):
    wd=int(len(label)*6.1+16)
    return wd,f'<span class="mono" style="position: absolute; left: {x}px; top: {y}px; width: {wd}px; height: 18px; box-sizing: border-box; display: inline-flex; align-items: center; justify-content: center; border-radius: 9px; background: {bg}; border: 1px solid {bd}; color: {col}; font-size: 10px; white-space: nowrap;">{label}</span>\n'
YR=720
for k,w in WT.items():
    sel=k in SEL
    if w['base']=='main':
        label=f'PR #{w["pr"]} → rust-backend'
        wd=int(len(label)*6.1+16)
        xc={0:230,1:578,2:926}[w['lane']]
        xt={0:450,1:578,2:706}[w['lane']]
        cy=w['bot']+8
        col='#6bfb9a' if sel else '#4ade80'; op='1' if sel else '0.6'; sw='2.4' if sel else '2'; mk='m-gs' if sel else 'm-g'
        if xc==xt:
            path=f'M{xc} {cy+18} V{MAIN["y"]}'
        else:
            sg=1 if xt>xc else -1
            path=f'M{xc} {cy+18} V{YR-10} Q{xc} {YR} {xc+sg*10} {YR} H{xt-sg*10} Q{xt} {YR} {xt} {YR+10} V{MAIN["y"]}'
        links.append(f'<path d="{path}" stroke="{col}" stroke-opacity="{op}" stroke-width="{sw}" fill="none" stroke-linecap="round" marker-end="url(#{mk})"></path>')
        _,h=chip(xc-wd//2,cy,label,'#6bfb9a' if sel else '#a4f0cd','rgba(74, 222, 128, 0.14)' if sel else '#251913','rgba(74, 222, 128, 0.55)' if sel else 'rgba(74, 222, 128, 0.35)')
        chips.append(h)
    else:
        b=WT[w['base']]
        lx=w['x']; rx=lx-14
        y0=w['y']+44; yb=b['y']+22
        col='#6bfb9a' if sel else '#a4f0cd'; op='1' if sel else '0.8'; sw='2.4' if sel else '2'; mk='m-ts' if sel else 'm-t'
        path=f'M{lx} {y0} H{rx+8} Q{rx} {y0} {rx} {y0+8} V{yb-8} Q{rx} {yb} {rx+8} {yb} H{lx}'
        links.append(f'<path d="{path}" stroke="{col}" stroke-opacity="{op}" stroke-width="{sw}" fill="none" stroke-linecap="round" marker-end="url(#{mk})"></path>')
        links.append(f'<circle cx="{lx}" cy="{y0}" r="3.5" fill="{col}" fill-opacity="{op}"></circle>')
        cy=w['bot']+8
        links.append(f'<path d="M{rx} {cy+9} H{lx-2}" stroke="{col}" stroke-opacity="{op}" stroke-width="{sw}" stroke-linecap="round"></path>')
        _,h=chip(lx-2,cy,f'PR #{w["pr"]} → {b["name"]}','#6bfb9a' if sel else '#a4f0cd','rgba(74, 222, 128, 0.14)' if sel else '#251913','rgba(74, 222, 128, 0.55)' if sel else 'rgba(164, 240, 205, 0.4)')
        chips.append(h)
# ---------- main (default branch) hub ----------
mx,my,mw,mh=MAIN['x'],MAIN['y'],MAIN['w'],MAIN['h']
def seg(c,label):
    return f'<div style="flex: 1 1 0; min-width: 0;"><div style="height: 4px; border-radius: 2px; background: {c};"></div><div class="mono" style="margin-top: 2px; font-size: 9px; line-height: 11px; color: #869486; white-space: nowrap;">{label}</div></div>'
hub=f'''<div style="position: absolute; left: {mx}px; top: {my}px; width: {mw}px; height: {mh}px; box-sizing: border-box; padding: 7px 12px; border-radius: 14px; background: #251913; border: 1px solid rgba(74, 222, 128, 0.55); box-shadow: 0 0 0 4px rgba(74, 222, 128, 0.08), inset 0 1px 0 0 rgba(74, 222, 128, 0.16), 0 14px 28px -16px rgba(10, 7, 5, 0.85);">
<div style="display: flex; align-items: center; justify-content: space-between; height: 20px;"><span class="mono" style="display: inline-flex; align-items: center; gap: 7px; font-size: 13px; font-weight: 600; color: #f5ded4;"><span style="display: inline-flex; color: #6bfb9a;">{svg(13,BRI)}</span>rust-backend<span style="padding: 0 6px; border-radius: 4px; background: rgba(74, 222, 128, 0.14); color: #6bfb9a; font-size: 9.5px; font-weight: 500; line-height: 16px; letter-spacing: 0.04em;">DEFAULT</span></span>{cibadge('ok',True)}</div>
<div style="display: flex; gap: 6px; margin-top: 5px;">{seg('#a4f0cd','lint 12s')}{seg('#a4f0cd','test 1m 04s')}{seg('#a4f0cd','build 2m')}{seg('#4ade80','deploy · running')}</div>
<div style="display: flex; align-items: center; justify-content: space-between; height: 22px; margin-top: 5px;"><span class="mono" style="display: inline-flex; align-items: center; gap: 5px; font-size: 10px; color: #6bfb9a;">{svg(11,PRI)}#29 merging<span style="color: #869486;">· 1 queued · 8 PRs target this</span></span><span style="display: inline-flex; gap: 3px;"><button type="button" class="ghost" style="display: inline-flex; align-items: center; gap: 5px; height: 20px; padding: 0 8px; border: 1px solid #3d271d; border-radius: 6px; background: #2b1d16; color: #bccabb; font-size: 10.5px;">Merge queue</button>{btn(I_PULL,'Pull latest')}</span></div>
</div>'''
log_link=f'<path d="M{int(log_right)+4} 791 H{mx}" stroke="#4ade80" stroke-opacity="0.6" stroke-width="1.5"></path>'
mk=lambda i,c,o: f'<marker id="{i}" markerUnits="userSpaceOnUse" markerWidth="12" markerHeight="12" refX="10" refY="6" orient="auto"><path d="M1 1.5 L10 6 L1 10.5 z" fill="{c}" fill-opacity="{o}"></path></marker>'
svgblock=f'''<svg width="{PW}" height="{PH}" viewBox="0 0 {PW} {PH}" fill="none" aria-hidden="true" style="position: absolute; inset: 0;">
<defs>{mk('m-g','#4ade80','0.8')}{mk('m-gs','#6bfb9a','1')}{mk('m-t','#a4f0cd','0.9')}{mk('m-ts','#6bfb9a','1')}</defs>
{log_link}
{''.join(links)}
</svg>'''
def lg(c,t,dash=False):
    return f'<div style="display: flex; align-items: center; gap: 8px; height: 16px;"><svg width="24" height="8" viewBox="0 0 24 8" fill="none" aria-hidden="true"><path d="M1 4 H20" stroke="{c}" stroke-width="2" stroke-linecap="round"></path><path d="M17 1 L22 4 L17 7 z" fill="{c}"></path></svg><span>{t}</span></div>'
legend=f'''<div class="mono" style="position: absolute; left: {PW-30-236}px; top: {PH-82}px; width: 236px; box-sizing: border-box; padding: 8px 12px; border-radius: 10px; background: rgba(37, 25, 19, 0.7); border: 1px solid #2f211a; font-size: 10px; color: #869486;">
{lg('#4ade80','merges into default')}{lg('#a4f0cd','stacked on a worktree')}<div style="margin-top: 4px; color: #6b5348;">↑ higher = farther from rust-backend</div></div>'''
inner=f'''
<!-- canvas objects: bonsai tucked in the corner (translucent), default-branch hub, 8 worktree groups in 3 stack lanes, process/agent nodes, hierarchy links -->
{pre}
{svgblock}
{hub}
{cards}
{''.join(chips)}
{legend}
'''
# swap graph
a=s.index('<div style="position: absolute; left: 428px; right: 16px; bottom: 324px; height: 560px; overflow-x: auto; overflow-y: hidden;">')
a2=s.index('<div style="position: relative; width: 1156px; height: 560px; margin: 0 auto;">',a)
a3=s.index('>',a2)+1
b=s.index('</div>\n</div>\n</section>',a3)
holder='<div style="position: absolute; left: 428px; right: 16px; top: 68px; bottom: 58px; overflow: auto;">'
s=s[:a]+holder+'\n'+f'<div style="position: relative; width: {PW}px; height: {PH}px; margin: 0 auto;">'+inner+s[b:]
s=s.replace('<!-- graph: worktrees up high, bonsai anchored just above the terminals -->','<!-- graph: full-height canvas (terminals collapsed to a slim bar) -->')
# ---------- terminals: collapsed bar ----------
t0=s.index('<!-- TERMINALS -->')
t1=s.index('</section>',t0)
old=s[t0:t1]
ol=old.split('\n')
title=[l for l in ol if 'Terminals<span' in l][0]
openbtn=[l for l in ol if l.startswith('<button type="button" class="ghost" style="display: inline-flex; align-items: center; gap: 5px; height: 22px;')][0]
tabs=lambda ab,name,dot,glow: f'<span style="display: inline-flex; align-items: center; gap: 6px; height: 22px; padding: 0 8px 0 5px; border-radius: 6px; background: #1c110b; border: 1px solid #342721; font-size: 11px; color: #bccabb; white-space: nowrap;"><span class="mono" style="width: 14px; height: 14px; display: grid; place-items: center; border-radius: 4px; background: #3f322b; font-size: 7.5px; font-weight: 600; color: #bccabb;">{ab}</span>{name}<span style="width: 6px; height: 6px; border-radius: 9999px; background: {dot}; box-shadow: 0 0 0 3px {glow};"></span></span>'
expand='<button type="button" aria-label="Expand terminals" title="Expand terminals" class="ghost" style="width: 22px; height: 22px; display: grid; place-items: center; border: 0; border-radius: 6px; background: transparent; color: #869486;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 15l6-6 6 6"></path></svg></button>'
bar=f'''<!-- TERMINALS (collapsed to a slim bar so the canvas gets the space) -->
<div style="position: absolute; left: 428px; right: 16px; bottom: 16px; height: 34px; z-index: 5; box-sizing: border-box; display: flex; align-items: center; justify-content: space-between; padding: 0 6px 0 12px; border-radius: 12px; background: rgba(37, 25, 19, 0.94); backdrop-filter: blur(14px); border: 1px solid #3d271d; box-shadow: inset 0 1px 0 0 rgba(74, 222, 128, 0.12), 0 16px 32px -12px rgba(10, 7, 5, 0.75);">
<div style="display: flex; align-items: center; gap: 14px; min-width: 0;">
{title}
<div style="display: flex; align-items: center; gap: 6px;">{tabs('AG','Load Performance','#4ade80','rgba(74, 222, 128, 0.2)')}{tabs('CL','Review changes','#d97746','rgba(217, 119, 70, 0.25)')}</div>
</div>
<div style="display: flex; align-items: center; gap: 2px;">
{openbtn}
<span style="width: 1px; height: 12px; margin: 0 3px; background: #3d271d;"></span>
{expand}
</div>
</div>

'''
s=s[:t0]+bar+s[t1:]
# ---------- branches island ----------
rows=[('rust-backend','',None,'default'),('├─ ','feat/passkeys','run',34),('│  └─ ','feat/review','run',26),('│     └─ ','feat/passkey-ui','ok',37),
      ('├─ ','fix/session-expiry','ok',29),('│  └─ ','refactor/token-store','bad',28),('│     └─ ','feat/token-rotation','run',38),
      ('└─ ','feat/react-init-time','bad',31),('   └─ ','perf/lazy-routes','ok',39)]
def brow(pre_,name,ci,pr):
    sel= name=='feat/review'; root= ci is None
    bg='background: rgba(74, 222, 128, 0.09);' if sel else ''
    nc='#f5ded4' if sel else ('#a4f0cd' if root else '#bccabb')
    wt='600' if (sel or root) else '400'
    if root:
        right='<span class="mono" style="padding: 0 6px; border-radius: 4px; background: rgba(74, 222, 128, 0.14); color: #6bfb9a; font-size: 9.5px; line-height: 16px;">default</span>'
        label_html=f'<span style="display: inline-block; width: 8px; height: 8px; margin-right: 8px; border-radius: 9999px; background: #4ade80; box-shadow: 0 0 0 3px rgba(74, 222, 128, 0.2);"></span>{pre_}'
    else:
        ic,fg,_,t=CI[ci]
        anim=' animation: bspin 1.2s linear infinite;' if ci=='run' else ''
        right=f'<span class="mono" style="display: inline-flex; align-items: center; gap: 8px; font-size: 10px; color: #869486;"><span style="display: inline-flex; color: {fg};{anim}">{svg(11,ic)}</span>#{pr}</span>'
        label_html=f'<span style="color: #5a6a5c; white-space: pre;">{pre_}</span>{name}'
    return f'<li class="row" style="display: flex; align-items: center; justify-content: space-between; height: 22px; padding: 0 8px; border-radius: 6px; {bg}"><span class="mono" style="min-width: 0; font-size: 11.5px; font-weight: {wt}; color: {nc}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">{label_html}</span>{right}</li>'
lis='\n'.join(brow(*r) for r in rows)
a=s.index('<!-- BRANCHES -->'); b=s.index('<!-- TERMINALS')
old=s[a:b]
h0=old.index('<label ')
l0=old.index('<ul ',h0); l1=old.index('</ul>',l0)+len('</ul>')
old=old[:l0]+f'<ul style="flex: 1 1 auto; min-height: 0; margin: 0; padding: 4px 6px; list-style: none; overflow: hidden;">\n{lis}\n</ul>'+old[l1:]
old=old.replace('<span>8 worktrees · 8 agents</span>','<span>8 worktrees · 9 agents</span>')
s=s[:a]+old+s[b:]
s=s.replace('<title>Bonsai — Workspace (branch hierarchy)</title>','<title>Bonsai — Workspace (canvas nodes)</title>')
open(p,'w').write(s)
for t in ['div','span','li','ul','svg','pre','article','path','button','section','aside']:
    print(t,len(re.findall(r'<'+t+r'[\s>]',s)),len(re.findall(r'</'+t+'>',s)))
c=json.load(open('artifact-files/2c92c6c8-dda8-43e4-9c97-d7e86f05fd7c/project/canvas.json'))
c['boards']['App9.dc.html']={"expand":"fill","h":960,"is_interactive":True,"title":"L · Workspace — canvas nodes","w":1600,"x":4560,"y":9776}
if 'App9.dc.html' not in c['order']: c['order'].append('App9.dc.html')
json.dump(c,open('canvas/project/canvas.json','w'),separators=(',',':'),ensure_ascii=False)
