const $ = (s) => document.querySelector(s);
let redes = [], marcadas = new Set(), extras = [], hosts = [], aberto = null, ordem = { col: 'ip', desc: false }, rodando = false;

function aviso(t, erro) {
  const a = $('#aviso'); a.textContent = t; a.className = 'aviso' + (erro ? ' erro' : ''); a.hidden = false;
  clearTimeout(aviso.t); aviso.t = setTimeout(() => (a.hidden = true), erro ? 7000 : 2500);
}
async function api(url, metodo = 'GET', corpo) {
  const r = await fetch(url, { method: metodo, headers: corpo ? { 'Content-Type': 'application/json' } : undefined, body: corpo ? JSON.stringify(corpo) : undefined });
  const d = await r.json().catch(() => ({}));
  if (!r.ok) { aviso(d.erro || 'Algo deu errado.', true); throw new Error(d.erro); }
  return d;
}
const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

// ---------- escolha das faixas ----------
function contar(faixa) {
  const m = faixa.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)\/(\d+)$/);
  if (m) { const b = +m[5]; return b >= 31 ? 2 ** (32 - b) : 2 ** (32 - b) - 2; }
  return null;
}
function desenharFaixas() {
  const el = $('#redes'); el.textContent = '';
  redes.forEach((r) => {
    const c = document.createElement('label'); c.className = 'chip' + (marcadas.has(r.faixa) ? ' marcado' : '');
    c.innerHTML = `<input type="checkbox" ${marcadas.has(r.faixa) ? 'checked' : ''} hidden><div><b>${esc(r.faixa)}</b><small>${esc(r.interface)} · este PC: ${esc(r.ip)}${r.principal ? ' · principal' : ''}</small></div>`;
    c.onclick = (e) => { e.preventDefault(); marcadas.has(r.faixa) ? marcadas.delete(r.faixa) : marcadas.add(r.faixa); desenharFaixas(); };
    el.append(c);
  });
  if (!redes.length) el.innerHTML = '<span class="pouco">Nenhuma rede detectada. Adicione uma faixa abaixo.</span>';
  const ex = $('#extras'); ex.textContent = '';
  extras.forEach((f, i) => {
    const c = document.createElement('div'); c.className = 'chip marcado';
    c.innerHTML = `<div><b>${esc(f)}</b></div><button title="Remover" aria-label="Remover">✕</button>`;
    c.querySelector('button').onclick = () => { extras.splice(i, 1); desenharFaixas(); };
    ex.append(c);
  });
  const todas = [...marcadas, ...extras]; let n = 0, ok = true;
  todas.forEach((f) => { const c = contar(f); if (c == null) ok = false; else n += c; });
  $('#total').textContent = todas.length ? (ok ? `${n.toLocaleString('pt-BR')} endereços a verificar` : `${todas.length} faixa(s) selecionada(s)`) : 'Selecione ao menos uma faixa.';
}
$('#formFaixa').onsubmit = (e) => {
  e.preventDefault();
  const v = $('#novaFaixa').value.trim(); if (!v) return;
  if (!/^\d{1,3}(\.\d{1,3}){3}(\/\d{1,2}|-\d{1,3}(\.\d{1,3}){0,3})?$/.test(v)) return aviso('Faixa inválida. Exemplos: 192.168.1.0/24, 192.168.1.10-50 ou 192.168.1.5', true);
  if (!extras.includes(v)) extras.push(v);
  $('#novaFaixa').value = ''; desenharFaixas();
};

// ---------- varredura ----------
$('#iniciar').onclick = async () => {
  const faixas = [...marcadas, ...extras];
  if (!faixas.length) return aviso('Selecione ou adicione ao menos uma faixa.', true);
  aberto = null;
  try { await api('/api/varredura', 'POST', { faixas, portas: document.querySelector('input[name=portas]:checked').value }); } catch { return; }
  atualizar();
};
$('#parar').onclick = async () => { await api('/api/varredura', 'DELETE'); atualizar(); };

async function atualizar() {
  let r; try { r = await api('/api/varredura'); } catch { return; }
  rodando = r.estado === 'rodando';
  $('#iniciar').hidden = rodando; $('#parar').hidden = !rodando;
  const pr = $('#progresso'); pr.hidden = r.estado === 'ociosa';
  if (r.total) {
    const p = Math.round((r.feitos / r.total) * 100);
    $('#barra').style.width = p + '%';
    $('#progressoTexto').textContent = rodando ? `Varrendo… ${r.feitos}/${r.total} (${p}%) · ${r.hosts.length} encontrado(s)` : `${r.estado === 'cancelada' ? 'Interrompida' : 'Concluída'} em ${Math.round(r.segundos)} s · ${r.hosts.length} aparelho(s)`;
  }
  $('#resultado').hidden = r.estado === 'ociosa';
  hosts = r.hosts;
  $('#tituloRes').textContent = `Aparelhos encontrados (${hosts.length})`;
  desenharTabela();
}

// ---------- tabela ----------
const ipNum = (ip) => ip.split('.').reduce((a, b) => a * 256 + +b, 0);
function valor(h, col) {
  switch (col) {
    case 'ip': return ipNum(h.ip);
    case 'portas': return h.portas.length;
    case 'rtt_ms': return h.rtt_ms || 1e9;
    default: return (h[col] || '').toLowerCase() || '￿';
  }
}
function desenharTabela() {
  const q = $('#busca').value.trim().toLowerCase();
  let lista = hosts.filter((h) => !q || JSON.stringify(h).toLowerCase().includes(q));
  lista.sort((a, b) => { const x = valor(a, ordem.col), y = valor(b, ordem.col); return (x < y ? -1 : x > y ? 1 : 0) * (ordem.desc ? -1 : 1); });
  document.querySelectorAll('#tabela th').forEach((th) => { th.className = th.dataset.col === ordem.col ? 'ord' + (ordem.desc ? ' desc' : '') : ''; });
  $('#vazio').hidden = lista.length > 0 || rodando;
  const corpo = $('#corpo'); corpo.textContent = '';
  lista.forEach((h) => {
    const tr = document.createElement('tr'); tr.className = 'host';
    const portas = h.portas.map((p) => `<span class="tag" title="${esc(p.servico)}">${p.porta}</span>`).join('');
    tr.innerHTML = `<td class="mono">${esc(h.ip)}</td><td>${esc(h.nome) || '<span class="pouco">—</span>'}</td><td class="mono">${esc(h.mac) || '<span class="pouco">—</span>'}</td>
      <td>${esc(h.fabricante) || '<span class="pouco">—</span>'}</td><td>${esc(h.tipo) || '<span class="pouco">—</span>'}</td>
      <td>${portas || (h.pronto ? '<span class="pouco">nenhuma</span>' : '<span class="carregando">verificando…</span>')}</td><td class="pouco">${h.rtt_ms ? h.rtt_ms.toFixed(1) + ' ms' : '—'}</td>`;
    tr.onclick = () => { aberto = aberto === h.ip ? null : h.ip; desenharTabela(); };
    corpo.append(tr);
    if (aberto === h.ip) corpo.append(detalhe(h));
  });
}
function detalhe(h) {
  const tr = document.createElement('tr'); tr.className = 'detalhe';
  const linhas = [['Endereço IP', h.ip], ['Nome (DNS)', h.nome_dns], ['Nome (mDNS/Bonjour)', h.nome_mdns], ['Nome (NetBIOS)', h.nome_netbios], ['Grupo de trabalho', h.grupo],
    ['MAC', h.mac], ['Fabricante', h.fabricante], ['Tipo provável', h.tipo], ['Sistema provável', h.so], ['TTL', h.ttl || ''], ['Tempo de resposta', h.rtt_ms ? h.rtt_ms.toFixed(1) + ' ms' : ''],
    ['Mesma rede deste PC', h.mesma_rede ? 'sim (MAC confiável)' : 'não (passa por roteador)'], ['Descoberto por', h.descoberto_por]];
  const grade = linhas.filter((l) => l[1] !== '' && l[1] != null).map((l) => `<div><b>${l[0]}</b>${esc(l[1])}</div>`).join('');
  const portas = h.portas.length ? h.portas.map((p) => `<div><span class="tag">${p.porta}</span> ${esc(p.servico)}${p.info ? ` — <span class="pouco">${esc(p.info)}</span>` : ''}</div>`).join('') : `<div class="pouco">${h.pronto ? 'Nenhuma porta aberta encontrada.' : 'Verificando portas…'}</div>`;
  tr.innerHTML = `<td colspan="7"><div class="det-grade">${grade}</div><div class="det-portas"><b>Portas abertas</b>${portas}</div></td>`;
  return tr;
}
document.querySelectorAll('#tabela th').forEach((th) => (th.onclick = () => {
  ordem = { col: th.dataset.col, desc: ordem.col === th.dataset.col ? !ordem.desc : false }; desenharTabela();
}));
$('#busca').oninput = desenharTabela;

// ---------- início ----------
(async () => {
  try {
    const d = await api('/api/redes');
    $('#pc').textContent = d.computador ? `Computador: ${d.computador}` : '';
    redes = d.redes || []; if (redes[0]) marcadas.add(redes[0].faixa);
  } catch {}
  desenharFaixas(); atualizar();
  setInterval(() => { if (rodando) atualizar(); }, 1000);
})();
