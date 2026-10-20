// Painel LANTV: sem dependências além do hls.js (prévia do canal).
const $ = (s) => document.querySelector(s);
let itens = [], ultimoItens = '', arrastando = null, config = null;

function aviso(texto, erro) {
  const a = $('#aviso');
  a.textContent = texto; a.className = 'aviso' + (erro ? ' erro' : ''); a.hidden = false;
  clearTimeout(aviso.t); aviso.t = setTimeout(() => (a.hidden = true), erro ? 6000 : 2500);
}

async function api(caminho, metodo = 'GET', corpo) {
  const r = await fetch(caminho, {
    method: metodo,
    headers: corpo ? { 'Content-Type': 'application/json' } : undefined,
    body: corpo ? JSON.stringify(corpo) : undefined,
  });
  const dados = await r.json().catch(() => ({}));
  if (!r.ok) { aviso(dados.erro || 'Algo deu errado.', true); throw new Error(dados.erro); }
  return dados;
}

const fmtTempo = (s) => {
  s = Math.round(s || 0);
  return s >= 60 ? `${Math.floor(s / 60)} min ${s % 60 ? (s % 60) + ' s' : ''}`.trim() : `${s} s`;
};

// ---------- estado geral ----------
async function atualizar() {
  let e;
  try { e = await api('/api/estado'); } catch { return pill('Sem contato com o PC', false); }
  const m = e.motor;
  config = e.config;

  document.querySelectorAll('#modos button').forEach((b) => b.classList.toggle('ativo', b.dataset.modo === m.modo));

  const noHdmi = m.no_ar === 'hdmi';
  $('#noar').textContent = noHdmi ? 'Aparelho HDMI' : 'Propagandas';
  let det = '';
  if (noHdmi) det = 'As TVs estão mostrando a imagem da placa de captura.';
  else if (m.tocando) det = `Tocando agora: ${m.tocando}`;
  if (m.modo === 'hdmi' && !m.hdmi_com_sinal) det += ' (HDMI forçado, mas sem imagem)';
  det += '\n' + (!m.hdmi_configurada ? 'Placa HDMI: não configurada (veja Configurações).'
    : m.hdmi_erro ? 'Placa HDMI: ' + m.hdmi_erro + '.'
    : m.hdmi_com_sinal ? 'Placa HDMI: recebendo imagem.' : 'Placa HDMI: sem imagem (aparelho desligado ou conteúdo protegido).');
  $('#detalhe').textContent = det;
  $('#detalhe').style.whiteSpace = 'pre-line';
  pill(noHdmi ? 'No ar: HDMI' : 'No ar: propagandas', true);

  const chave = JSON.stringify(e.itens);
  if (chave !== ultimoItens && !arrastando && !document.activeElement?.closest?.('.item')) {
    ultimoItens = chave; itens = e.itens; desenhar();
  }
}

function pill(texto, ok) {
  const p = $('#pill'); p.textContent = texto; p.className = 'pill ' + (ok ? 'ok' : 'ruim');
}

document.querySelectorAll('#modos button').forEach((b) => (b.onclick = async () => {
  await api('/api/modo', 'POST', { modo: b.dataset.modo }); atualizar();
}));

// ---------- lista de propagandas ----------
function desenhar() {
  const ul = $('#lista'); ul.textContent = '';
  $('#vazio').hidden = itens.length > 0;
  itens.forEach((it, i) => ul.appendChild(linha(it, i)));
}

function el(tag, props = {}, ...filhos) {
  const e = Object.assign(document.createElement(tag), props);
  filhos.flat().forEach((f) => e.append(f));
  return e;
}

function linha(it, i) {
  const li = el('li', { className: 'item' + (it.ativo ? '' : ' inativo'), draggable: false });
  li.dataset.id = it.id;

  const alca = el('span', { className: 'alca', title: 'Arraste para mudar a ordem', textContent: '⠿' });
  alca.onmousedown = () => (li.draggable = true);
  li.ondragstart = (ev) => { arrastando = it.id; li.classList.add('arrastando'); ev.dataTransfer.effectAllowed = 'move'; ev.dataTransfer.setData('text/plain', it.id); };
  li.ondragend = () => { arrastando = null; li.draggable = false; li.classList.remove('arrastando'); document.querySelectorAll('.alvo').forEach((x) => x.classList.remove('alvo')); };
  li.ondragover = (ev) => { if (arrastando && arrastando !== it.id) { ev.preventDefault(); li.classList.add('alvo'); } };
  li.ondragleave = () => li.classList.remove('alvo');
  li.ondrop = (ev) => {
    ev.preventDefault();
    const ids = itens.map((x) => x.id).filter((x) => x !== arrastando);
    ids.splice(ids.indexOf(it.id), 0, arrastando);
    mover(ids);
  };

  const mini = it.miniatura
    ? el('img', { className: 'miniatura', src: it.miniatura, alt: '', loading: 'lazy' })
    : el('div', { className: 'miniatura' });

  const meta = el('div', { className: 'meta' });
  if (it.estado === 'preparando') meta.append(el('span', { className: 'estado-preparando', textContent: 'Preparando para o canal…' }));
  else if (it.estado === 'erro') meta.append(el('span', { className: 'estado-erro', textContent: 'Não foi possível usar este arquivo' }));
  else if (it.imagem) {
    const n = el('input', { type: 'number', min: 3, max: 120, value: it.segundos, title: 'Segundos na tela' });
    n.onchange = () => alterar(it.id, { segundos: parseInt(n.value, 10) });
    meta.append('Imagem · fica ', n, ' s na tela');
  } else meta.append(`Vídeo · ${fmtTempo(it.duracao)}`);
  if (!it.ativo) meta.append(el('span', { textContent: 'Pausada (não toca)' }));

  const nome = el('div', { className: 'nome', textContent: it.nome, title: 'Clique para renomear' });
  nome.onclick = () => {
    const novo = prompt('Nome da propaganda:', it.nome);
    if (novo && novo.trim() && novo !== it.nome) alterar(it.id, { nome: novo });
  };

  const chave = el('label', { className: 'chave', title: it.ativo ? 'Tocando — clique para pausar' : 'Pausada — clique para voltar a tocar' },
    el('input', { type: 'checkbox', checked: it.ativo }), el('span'));
  chave.querySelector('input').onchange = (ev) => alterar(it.id, { ativo: ev.target.checked });

  const sobe = el('button', { textContent: '▲', title: 'Subir', disabled: i === 0 });
  const desce = el('button', { textContent: '▼', title: 'Descer', disabled: i === itens.length - 1 });
  sobe.onclick = () => trocar(i, i - 1); desce.onclick = () => trocar(i, i + 1);
  const excluir = el('button', { className: 'excluir', textContent: '🗑', title: 'Remover' });
  excluir.onclick = async () => {
    if (!confirm(`Remover “${it.nome}”? Isso não pode ser desfeito.`)) return;
    itens = await api('/api/item/' + it.id, 'DELETE'); ultimoItens = ''; desenhar(); atualizar();
  };

  li.append(alca, mini, el('div', {}, nome, meta), el('div', { className: 'acoes' }, chave, sobe, desce, excluir));
  return li;
}

async function alterar(id, mudanca) {
  try { itens = await api('/api/item/' + id, 'PATCH', mudanca); } catch { /* aviso já mostrado */ }
  ultimoItens = ''; atualizar();
}
function trocar(a, b) { const ids = itens.map((x) => x.id); [ids[a], ids[b]] = [ids[b], ids[a]]; mover(ids); }
async function mover(ids) { itens = await api('/api/ordem', 'POST', { ids }); ultimoItens = ''; desenhar(); atualizar(); }

// ---------- envio de arquivos ----------
const drop = $('#drop'), entrada = $('#arquivos');
['dragenter', 'dragover'].forEach((t) => drop.addEventListener(t, (e) => { if (e.dataTransfer?.types?.includes('Files')) { e.preventDefault(); drop.classList.add('sobre'); } }));
['dragleave', 'drop'].forEach((t) => drop.addEventListener(t, () => drop.classList.remove('sobre')));
drop.addEventListener('drop', (e) => { if (e.dataTransfer?.files?.length) { e.preventDefault(); enviar([...e.dataTransfer.files]); } });
drop.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); entrada.click(); } });
entrada.onchange = () => { enviar([...entrada.files]); entrada.value = ''; };

function enviar(arquivos) {
  if (!arquivos.length) return;
  const form = new FormData();
  arquivos.forEach((f) => form.append('arquivo', f, f.name));
  const xhr = new XMLHttpRequest();
  const barra = $('#barra'), texto = $('#progressoTexto');
  $('#progresso').hidden = false; barra.style.width = '0'; texto.textContent = `Enviando ${arquivos.length} arquivo(s)…`;
  xhr.upload.onprogress = (e) => {
    if (!e.lengthComputable) return;
    const p = Math.round((e.loaded / e.total) * 100);
    barra.style.width = p + '%'; texto.textContent = p < 100 ? `Enviando… ${p}%` : 'Processando…';
  };
  xhr.onload = () => {
    $('#progresso').hidden = true;
    let r = {}; try { r = JSON.parse(xhr.responseText); } catch {}
    if (xhr.status !== 200) return aviso(r.erro || 'O envio falhou.', true);
    if (r.recusados?.length) aviso('Formato não aceito: ' + r.recusados.join(', '), true);
    else aviso(`${r.enviados} propaganda(s) enviada(s). Preparando para o canal…`);
    atualizar();
  };
  xhr.onerror = () => { $('#progresso').hidden = true; aviso('O envio falhou. Confira a conexão com o PC.', true); };
  xhr.open('POST', '/api/upload'); xhr.send(form);
}

// ---------- endereços das TVs ----------
$('#urlCanal').textContent = `${location.origin}/canal/canal.m3u8`;
$('#urlTv').textContent = `${location.origin}/tv`;
document.querySelectorAll('[data-copiar]').forEach((b) => (b.onclick = async () => {
  const t = $('#' + b.dataset.copiar).textContent;
  try { await navigator.clipboard.writeText(t); aviso('Endereço copiado.'); } catch { prompt('Copie o endereço:', t); }
}));

// ---------- configurações ----------
function opcoes(sel, lista, atual, vazio) {
  sel.textContent = '';
  sel.append(el('option', { value: '', textContent: vazio }));
  const todos = new Set(lista); if (atual) todos.add(atual);
  todos.forEach((n) => sel.append(el('option', { value: n, textContent: n })));
  sel.value = atual || '';
}
async function carregarConfig(procurar) {
  const [c, d] = await Promise.all([api('/api/config'), api('/api/dispositivos')]);
  config = c;
  $('#cfgQualidade').value = c.qualidade;
  $('#cfgCodificador').value = c.codificador;
  $('#cfgHdmi4k').checked = c.hdmi_4k;
  opcoes($('#cfgHdmi'), d.video, c.hdmi, 'Não usar placa de captura');
  opcoes($('#cfgAudio'), d.audio, c.hdmi_audio, 'Sem áudio da placa');
  if (procurar) aviso(`${d.video.length} placa(s) de vídeo e ${d.audio.length} entrada(s) de áudio encontradas.`);
}
$('#detalhesConfig').addEventListener('toggle', (e) => { if (e.target.open) carregarConfig(); });
$('#atualizarDisp').onclick = () => carregarConfig(true);
$('#formConfig').onsubmit = async (e) => {
  e.preventDefault();
  await api('/api/config', 'PUT', {
    qualidade: $('#cfgQualidade').value, codificador: $('#cfgCodificador').value,
    hdmi: $('#cfgHdmi').value, hdmi_audio: $('#cfgAudio').value, hdmi_4k: $('#cfgHdmi4k').checked,
  });
  aviso('Salvo. O canal reinicia por alguns segundos.'); atualizar();
};

// ---------- prévia do canal ----------
(function previa() {
  const v = $('#v'), url = '/canal/canal.m3u8';
  function iniciar() {
    if (window.Hls && Hls.isSupported()) {
      const h = new Hls({ liveSyncDurationCount: 3, manifestLoadingMaxRetry: Infinity });
      h.loadSource(url); h.attachMedia(v);
      h.on(Hls.Events.ERROR, (_, d) => { if (d.fatal) setTimeout(() => { h.destroy(); iniciar(); }, 3000); });
    } else v.src = url;
    v.play().catch(() => {});
  }
  iniciar();
  v.onclick = () => { v.muted = !v.muted; };
})();

atualizar(); setInterval(atualizar, 2000);
