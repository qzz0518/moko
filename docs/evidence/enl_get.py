# READ-ONLY ECHONET Lite probe. Only sends ESV 0x62 (Get). Never sends Set (0x60/0x61/0x6E).
import socket, sys, time, struct
HOST=sys.argv[1]; PORT=3610
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
try: s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEPORT,1)
except Exception: pass
s.bind(('',PORT)); s.settimeout(0.3)
s.setsockopt(socket.IPPROTO_IP,socket.IP_ADD_MEMBERSHIP,struct.pack('4s4s',socket.inet_aton('224.0.23.0'),socket.inet_aton('0.0.0.0')))
tid=100
def parse(d):
    if len(d)<12 or d[:2]!=b'\x10\x81': return None
    seoj,deoj,esv,opc=d[4:7],d[7:10],d[10],d[11]
    i=12;props=[]
    for _ in range(opc):
        if i+2>len(d): break
        e,p=d[i],d[i+1];props.append((e,d[i+2:i+2+p]));i+=2+p
    return dict(tid=int.from_bytes(d[2:4],'big'),seoj=seoj.hex(),deoj=deoj.hex(),esv=esv,props=props)
def get(deoj,epc,timeout=2.0):
    global tid
    tid+=1
    f=bytes.fromhex('1081')+tid.to_bytes(2,'big')+bytes.fromhex('05FF01')+bytes.fromhex(deoj)+bytes([0x62,1,epc,0])
    t0=time.time();s.sendto(f,(HOST,PORT))
    while time.time()-t0<timeout:
        try:d,a=s.recvfrom(2048)
        except socket.timeout: continue
        if a[0]!=HOST: continue
        m=parse(d)
        if m and m['tid']==tid: return m['esv'],m['props'],time.time()-t0
    return None,[],None
def decode_map(b):
    if not b: return []
    n=b[0]
    if n<16: return list(b[1:1+n])
    r=[]
    for i,x in enumerate(b[1:17]):
        for j in range(8):
            if x>>j&1: r.append(((j+8)<<4)|i)
    return sorted(r)
def show(b):
    try: t=b.decode('ascii').rstrip('\x00 ');
    except Exception: t=None
    return b.hex()+(f"  ascii={t!r}" if t and all(32<=ord(c)<127 for c in t) else '')
print('== Node profile 0EF001')
for e in [0x80,0x82,0x83,0x88,0x8A,0x8B,0x8C,0x8D,0x8E,0xD3,0xD4,0xD5,0xD6,0xD7]:
    esv,p,l=get('0EF001',e)
    print(f'EPC {e:02X} esv={esv and hex(esv)}',show(p[0][1]) if p else None, f'{l*1000:.0f}ms' if l else 'timeout'); time.sleep(0.15)
for obj in sys.argv[2:]:
    print('== Object',obj)
    maps={}
    for e,n in [(0x9D,'INF'),(0x9E,'SET'),(0x9F,'GET')]:
        esv,p,l=get(obj,e)
        maps[n]=decode_map(p[0][1]) if p else []
        print(n,'map',[f'{x:02X}' for x in maps[n]]); time.sleep(0.15)
    for e in maps['GET']:
        if e in (0x9D,0x9E,0x9F): continue
        esv,p,l=get(obj,e)
        print(f'EPC {e:02X} esv={esv and hex(esv)}',show(p[0][1]) if p else None, 'SET' if e in maps['SET'] else '', 'INF' if e in maps['INF'] else '', f'{l*1000:.0f}ms' if l else 'timeout')
        time.sleep(0.15)
print('== listening for unsolicited INF for 20s')
end=time.time()+20
while time.time()<end:
    try:d,a=s.recvfrom(2048)
    except socket.timeout: continue
    if a[0]!=HOST: continue
    m=parse(d)
    if m: print(time.strftime('%H:%M:%S'),'esv',hex(m['esv']),'seoj',m['seoj'],[(f'{e:02X}',v.hex()) for e,v in m['props']])
