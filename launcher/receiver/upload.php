<?php
// Launcher Brasil — base de eventos no seu site.
// Gerado pelo painel do dono da base. Envie para uma pasta do site
// (ex.: public_html/ac/upload.php). Pacotes ficam em ./files/ (público);
// eventos e hosts ficam em ./data/ (protegido).
// A chave do dono não está neste arquivo, só o hash dela; não edite.
const OWNER_KEY_SHA256 = '{{KEY_SHA256}}';
const MAX_FILE_BYTES = 21474836480; // 20 GB por pacote
const MAX_EVENT_BYTES = 524288;

header('Cache-Control: no-store');
header('X-Content-Type-Options: nosniff');

function out($code, $data) {
    http_response_code($code);
    header('Content-Type: application/json; charset=utf-8');
    echo json_encode($data, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
    exit;
}

$root = __DIR__;
$files = $root . '/files';
$data = $root . '/data';
foreach ([$files, $data, $data . '/events'] as $d) {
    if (!is_dir($d) && !@mkdir($d, 0755, true)) {
        out(500, ['error' => 'não foi possível criar as pastas ao lado do upload.php']);
    }
}
if (!is_file($files . '/.htaccess')) {
    @file_put_contents($files . '/.htaccess', "Options -Indexes\n<FilesMatch \"\\.(php[0-9]?|phtml|phar|pht|cgi|pl|py|sh)(\\.|$)\">\n  Require all denied\n</FilesMatch>\n<IfModule mod_php.c>\n  php_flag engine off\n</IfModule>\n");
}
if (!is_file($data . '/.htaccess')) {
    @file_put_contents($data . '/.htaccess', "Require all denied\nDeny from all\n");
}

// Data files are PHP files that exit when requested directly, so they stay
// private even where .htaccess is ignored.
const GUARD = "<?php http_response_code(404); exit; ?>\n";
function readData($path, $default) {
    if (!is_file($path)) return $default;
    $raw = file_get_contents($path);
    if (strpos($raw, GUARD) === 0) $raw = substr($raw, strlen(GUARD));
    $v = json_decode($raw, true);
    return is_array($v) ? $v : $default;
}
function writeData($path, $value) {
    $tmp = $path . '.tmp' . bin2hex(random_bytes(4));
    if (file_put_contents($tmp, GUARD . json_encode($value, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES), LOCK_EX) === false || !rename($tmp, $path)) {
        @unlink($tmp);
        out(500, ['error' => 'não foi possível gravar no site']);
    }
}
function body() {
    $raw = file_get_contents('php://input', false, null, 0, MAX_EVENT_BYTES + 1);
    if (strlen($raw) > MAX_EVENT_BYTES) out(413, ['error' => 'dados grandes demais']);
    $v = json_decode($raw, true);
    if (!is_array($v)) out(400, ['error' => 'dados inválidos']);
    return $v;
}
function str($v, $max) {
    return is_string($v) ? mb_substr(trim($v), 0, $max) : '';
}

$base = readData($data . '/base.php', ['name' => 'Base de eventos', 'description' => '']);
$hosts = readData($data . '/hosts.php', []);

function who($hosts, $base) {
    $key = isset($_SERVER['HTTP_X_KEY']) ? (string)$_SERVER['HTTP_X_KEY'] : '';
    if ($key === '') return null;
    $h = hash('sha256', $key);
    if (hash_equals(OWNER_KEY_SHA256, $h)) return ['role' => 'owner', 'id' => 'owner', 'name' => $base['name']];
    foreach ($hosts as $id => $info) {
        if (hash_equals($info['keyHash'], $h)) return ['role' => 'host', 'id' => $id, 'name' => $info['name']];
    }
    return null;
}
function need($me) {
    if (!$me) out(403, ['error' => 'chave inválida — peça um novo código de host ao dono da base']);
    return $me;
}
function owner($me) {
    if (need($me)['role'] !== 'owner') out(403, ['error' => 'só o dono da base pode fazer isso']);
    return $me;
}

$validId = '/^[a-z0-9][a-z0-9\-]{0,80}$/';
function eventPath($id) { global $data; return $data . '/events/' . $id . '.php'; }
function allEvents() {
    global $data;
    $out = [];
    foreach (glob($data . '/events/*.php') ?: [] as $f) {
        $e = readData($f, null);
        if ($e) $out[] = $e;
    }
    usort($out, function ($a, $b) { return strcmp($b['updatedAt'] ?? '', $a['updatedAt'] ?? ''); });
    return $out;
}
function fullView($e) {
    unset($e['passHash']);
    $e['locked'] = false;
    return $e;
}
function publicView($e) {
    if (empty($e['private'])) return fullView($e);
    $slim = function ($c) { return ['id' => $c['id'] ?? '', 'name' => $c['name'] ?? '', 'layout' => $c['layout'] ?? '', 'image' => $c['image'] ?? '']; };
    return [
        'id' => $e['id'], 'host' => $e['host'], 'name' => $e['name'], 'description' => $e['description'] ?? '',
        'private' => true, 'locked' => true, 'lan' => !empty($e['lan']), 'updatedAt' => $e['updatedAt'] ?? '',
        'server' => new stdClass(), 'track' => $slim($e['track'] ?? []), 'cars' => array_map($slim, $e['cars'] ?? []),
    ];
}

$me = who($hosts, $base);
$action = isset($_GET['action']) ? (string)$_GET['action'] : '';

switch ($action) {
    case 'hub': // public: what the players' launcher reads
        $scheme = (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off') || (($_SERVER['HTTP_X_FORWARDED_PROTO'] ?? '') === 'https') ? 'https' : 'http';
        $dir = rtrim(str_replace('\\', '/', dirname($_SERVER['SCRIPT_NAME'])), '/');
        out(200, [
            'name' => $base['name'], 'description' => $base['description'] ?? '',
            'filesUrl' => $scheme . '://' . $_SERVER['HTTP_HOST'] . $dir . '/files',
            'events' => array_map('publicView', allEvents()),
        ]);

    case 'unlock': // public: password of a private event
        $in = body();
        $id = str($in['event'] ?? '', 90);
        $e = preg_match($validId, $id) ? readData(eventPath($id), null) : null;
        if (!$e || empty($e['private']) || !password_verify((string)($in['password'] ?? ''), $e['passHash'] ?? '')) {
            sleep(1);
            out(403, ['error' => 'senha incorreta']);
        }
        out(200, fullView($e));

    case 'ping':
        $me = need($me);
        out(200, ['ok' => true, 'role' => $me['role'], 'hostId' => $me['id'], 'name' => $me['name'], 'base' => $base['name']]);

    case 'my-events':
        $me = need($me);
        $mine = array_values(array_filter(allEvents(), function ($e) use ($me) { return $me['role'] === 'owner' || $e['hostId'] === $me['id']; }));
        out(200, ['events' => array_map(function ($e) { $v = fullView($e); $v['hasPassword'] = !empty($e['passHash']); return $v; }, $mine)]);

    case 'event-save':
        $me = need($me);
        $in = body();
        $id = str($in['id'] ?? '', 90);
        if (!preg_match($validId, $id)) out(400, ['error' => 'id de evento inválido']);
        $old = readData(eventPath($id), null);
        if ($old && $old['hostId'] !== $me['id'] && $me['role'] !== 'owner') out(403, ['error' => 'este evento é de outro host']);
        $name = str($in['name'] ?? '', 120);
        if ($name === '') out(400, ['error' => 'o evento precisa de um nome']);
        $e = [
            'id' => $id,
            'hostId' => $old ? $old['hostId'] : $me['id'],
            'host' => $old && $old['hostId'] !== $me['id'] ? $old['host'] : $me['name'],
            'name' => $name,
            'description' => str($in['description'] ?? '', 600),
            'private' => !empty($in['private']),
            'lan' => !empty($in['lan']),
            'invite' => str($in['invite'] ?? '', 2000),
            'server' => ['host' => str($in['server']['host'] ?? '', 255), 'httpPort' => (int)($in['server']['httpPort'] ?? 0)],
            'track' => is_array($in['track'] ?? null) ? $in['track'] : [],
            'cars' => is_array($in['cars'] ?? null) ? array_slice(array_values($in['cars']), 0, 300) : [],
            'createdAt' => $old['createdAt'] ?? gmdate('c'),
            'updatedAt' => gmdate('c'),
        ];
        if ($e['lan']) {
            $e['radmin'] = ['network' => str($in['radmin']['network'] ?? '', 120), 'password' => str($in['radmin']['password'] ?? '', 120)];
        }
        if ($e['private']) {
            $pw = (string)($in['password'] ?? '');
            if ($pw !== '') $e['passHash'] = password_hash($pw, PASSWORD_DEFAULT);
            elseif (!empty($old['passHash'])) $e['passHash'] = $old['passHash'];
            else out(400, ['error' => 'evento privado precisa de senha']);
        }
        writeData(eventPath($id), $e);
        out(200, ['ok' => true, 'event' => fullView($e)]);

    case 'event-delete':
        $me = need($me);
        $in = body();
        $id = str($in['id'] ?? '', 90);
        $old = preg_match($validId, $id) ? readData(eventPath($id), null) : null;
        if (!$old) out(200, ['ok' => true]);
        if ($old['hostId'] !== $me['id'] && $me['role'] !== 'owner') out(403, ['error' => 'este evento é de outro host']);
        @unlink(eventPath($id));
        out(200, ['ok' => true]);

    case 'base-save':
        owner($me);
        $in = body();
        $base = ['name' => str($in['name'] ?? '', 80) ?: 'Base de eventos', 'description' => str($in['description'] ?? '', 300)];
        writeData($data . '/base.php', $base);
        out(200, ['ok' => true]);

    case 'host-list':
        owner($me);
        $counts = [];
        foreach (allEvents() as $e) $counts[$e['hostId']] = ($counts[$e['hostId']] ?? 0) + 1;
        $list = [];
        foreach ($hosts as $id => $h) $list[] = ['id' => $id, 'name' => $h['name'], 'createdAt' => $h['createdAt'], 'events' => $counts[$id] ?? 0];
        out(200, ['hosts' => $list]);

    case 'host-add':
        owner($me);
        $in = body();
        $name = str($in['name'] ?? '', 60);
        if ($name === '') out(400, ['error' => 'dê um nome ao host']);
        $key = bin2hex(random_bytes(24));
        $id = 'h' . bin2hex(random_bytes(5));
        $hosts[$id] = ['name' => $name, 'keyHash' => hash('sha256', $key), 'createdAt' => gmdate('c')];
        writeData($data . '/hosts.php', $hosts);
        out(200, ['id' => $id, 'key' => $key]);

    case 'host-remove':
        owner($me);
        $in = body();
        $id = str($in['id'] ?? '', 40);
        unset($hosts[$id]);
        writeData($data . '/hosts.php', $hosts);
        foreach (allEvents() as $e) {
            if ($e['hostId'] === $id) @unlink(eventPath($e['id']));
        }
        out(200, ['ok' => true]);
}

// ---- packages ----------------------------------------------------------------
// Names carry the content hash (car-<id>-<hash12>.zip), so files are never
// replaced by a different version: each event keeps pointing at its own.

$me = need($me);
$name = isset($_GET['file']) ? (string)$_GET['file'] : '';
if (!preg_match('/^(?:(?:car|track)-[A-Za-z0-9_.\-]{1,128}-[0-9a-f]{12}\.zip|img-(?:car|track)-[A-Za-z0-9_.\-]{1,128}-[0-9a-f]{12}\.(?:jpg|jpeg|png|webp))$/', $name)
    || preg_match('/\.(php[0-9]?|phtml|phar|pht|cgi|pl|py|sh|asp|aspx|jsp|htaccess)(\.|$)/i', $name)) {
    out(400, ['error' => 'nome de arquivo inválido']);
}
$final = $files . '/' . $name;
$part = $final . '.part';
clearstatcache();

switch ($action) {
    case 'status':
        out(200, ['part' => is_file($part) ? filesize($part) : 0, 'size' => is_file($final) ? filesize($final) : -1]);

    case 'chunk':
        if ($_SERVER['REQUEST_METHOD'] !== 'POST') out(405, ['error' => 'use POST']);
        if (is_file($final)) out(200, ['part' => filesize($final), 'exists' => true]);
        $offset = isset($_GET['offset']) ? (int)$_GET['offset'] : -1;
        $current = is_file($part) ? filesize($part) : 0;
        if ($offset !== 0 && $offset !== $current) out(409, ['error' => 'posição inesperada', 'part' => $current]);
        $in = fopen('php://input', 'rb');
        $fp = fopen($part, $offset === 0 ? 'wb' : 'ab');
        if (!$in || !$fp) out(500, ['error' => 'não foi possível gravar']);
        $n = stream_copy_to_stream($in, $fp);
        fclose($fp);
        fclose($in);
        $size = ($offset === 0 ? 0 : $current) + $n;
        if ($size > MAX_FILE_BYTES) {
            @unlink($part);
            out(413, ['error' => 'arquivo grande demais']);
        }
        out(200, ['part' => $size]);

    case 'finish':
        $want = strtolower(isset($_GET['sha256']) ? (string)$_GET['sha256'] : '');
        if (is_file($final)) {
            @unlink($part);
            if ($want !== '' && !hash_equals($want, hash_file('sha256', $final))) out(409, ['error' => 'já existe um arquivo diferente com esse nome']);
            out(200, ['ok' => true, 'size' => filesize($final)]);
        }
        if (!is_file($part)) out(404, ['error' => 'envio não encontrado']);
        if ($want === '' || !hash_equals($want, hash_file('sha256', $part))) {
            @unlink($part);
            out(422, ['error' => 'o arquivo chegou corrompido (sha256 não confere)']);
        }
        if (!rename($part, $final)) out(500, ['error' => 'não foi possível finalizar o arquivo']);
        out(200, ['ok' => true, 'size' => filesize($final)]);

    case 'delete':
        owner($me);
        @unlink($final);
        out(200, ['ok' => true]);
}

out(400, ['error' => 'ação desconhecida']);
