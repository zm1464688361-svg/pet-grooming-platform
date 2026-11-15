/**
 * 共享工具：演示登录态 + API 封装 + 错误提示。
 * 响应结构：{"code":0,"message":"ok","data":...}（code=0 成功；非 0 为错误码，message 为服务端固定文案）
 */
const C端用户 = localStorage.getItem('demo_user') || 'user_1001';
const B端员工 = localStorage.getItem('demo_staff') || 'manager_01';
const B端员工名 = { manager_01: '王店长（店长）', front_01: '刘前台（前台）', svc_01: '陈师傅（服务）', cs_01: '赵客服（客服）' };

async function api(path, { method = 'GET', body = null, role = 'user' } = {}) {
  const headers = { 'Content-Type': 'application/json' };
  headers[role === 'user' ? 'X-User-Id' : 'X-Staff-Id'] = role === 'user' ? C端用户 : B端员工;
  const opt = { method, headers };
  if (body) opt.body = JSON.stringify(body);
  const res = await fetch(path, opt);
  const json = await res.json().catch(() => ({ code: 50000, message: '网络异常，请稍后重试' }));
  return json;
}

function toast(msg, ok = true) {
  let el = document.getElementById('_toast');
  if (!el) {
    el = document.createElement('div');
    el.id = '_toast';
    el.style.cssText = 'position:fixed;top:60px;left:50%;transform:translateX(-50%);z-index:999;padding:10px 18px;border-radius:14px;font-size:13px;color:#fff;box-shadow:0 8px 20px rgba(0,0,0,.18);max-width:320px;text-align:center';
    document.body.appendChild(el);
  }
  el.style.background = ok ? '#38a878' : '#e05a4b';
  el.textContent = msg;
  clearTimeout(el._t);
  el._t = setTimeout(() => (el.style.display = 'none'), 2600);
  el.style.display = 'block';
}

async function call(path, opts, onOk) {
  const r = await api(path, opts);
  if (r.code === 0) {
    if (onOk) onOk(r.data, r.message);
    return r;
  }
  toast(r.message || '操作失败', false);
  throw r;
}

const fmtYuan = c => (c / 100).toFixed(2);
const pad = n => String(n).padStart(2, '0');
const fmtDT = s => { const d = new Date(s); return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`; };
const fmtHM = s => { const d = new Date(s); return `${pad(d.getHours())}:${pad(d.getMinutes())}`; };
const STATUS = { 1: '待支付', 2: '已确认', 3: '取消审核中', 4: '已取消', 5: '已核销', 6: '服务中', 7: '已完成', 8: '未到店', 9: '已关闭' };
