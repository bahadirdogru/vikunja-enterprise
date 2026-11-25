import json,sys,urllib.request
base=sys.argv[1]
def call(m,p,body=None,tok=None):
    r=urllib.request.Request(base+p,method=m,data=json.dumps(body).encode() if body is not None else None,headers={'Content-Type':'application/json',**({'Authorization':'Bearer '+tok} if tok else {})})
    try:
        with urllib.request.urlopen(r) as x: return x.status,json.loads(x.read() or b'null')
    except urllib.error.HTTPError as e:
        return e.code,e.read()[:200].decode(errors='replace')
s,_=call('POST','/api/v1/register',{'username':'smoke','email':'smoke@example.com','password':'Sm0ke-test-123456'}); print('register',s)
s,d=call('POST','/api/v1/login',{'username':'smoke','password':'Sm0ke-test-123456'}); print('login',s); t=d['token']
s,p=call('PUT','/api/v1/projects',{'title':'Deneme'},t); print('project',s); pid=p['id']
s,k=call('PUT',f'/api/v1/projects/{pid}/tasks',{'title':'İlk görev'},t); print('task',s)
for name,path in [('custom fields',f'/api/v2/projects/{pid}/custom-fields'),('workflow',f'/api/v2/projects/{pid}/workflow'),('approvals',f'/api/v2/approvals'),('automation rules',f'/api/v2/projects/{pid}/automations'),('dashboards','/api/v2/dashboards'),('forms',f'/api/v2/projects/{pid}/forms')]:
    s,d=call('GET',path,None,t); print(f'{name:18}',s,(d if s>=400 else ''))
s,d=call('POST',f'/api/v2/projects/{pid}/custom-fields',{'title':'Bütçe','type':'number'},t); print('create field',s, d if s>=400 else '')
