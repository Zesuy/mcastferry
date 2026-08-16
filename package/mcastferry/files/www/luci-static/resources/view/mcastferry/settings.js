'use strict';
'require view';
'require form';
'require rpc';
'require ui';
'require tools.widgets as widgets';

const callStatus = rpc.declare({
	object: 'mcastferry',
	method: 'status',
	expect: { '': {} }
});

const callInstallPlaylist = rpc.declare({
	object: 'mcastferry',
	method: 'install_playlist',
	expect: { '': {} }
});

return view.extend({
	load: function() {
		return L.resolveDefault(callStatus(), {});
	},

	render: function(status) {
		var m, s, o;
		var service = status.service || {};
		var instances = service.instances || {};
		var running = Object.keys(instances).some(function(name) {
			return instances[name] && instances[name].running;
		});

		m = new form.Map('mcastferry', _('McastFerry'),
			_('IPv4 multicast to close-delimited HTTP relay.'));

		s = m.section(form.TypedSection, 'mcastferry', _('Service'));
		s.anonymous = true;
		s.addremove = false;

		o = s.option(form.DummyValue, '_runtime', _('Runtime status'));
		o.cfgvalue = function() {
			return running ? _('Running') : _('Stopped');
		};

		o = s.option(form.Flag, 'enabled', _('Enable'));
		o.default = '0';
		o.rmempty = false;

		o = s.option(widgets.NetworkSelect, 'multicast_input', _('Multicast input network'));
		o.nocreate = true;
		o.rmempty = false;

		o = s.option(form.Value, 'http_listen', _('HTTP listen address'));
		o.datatype = 'ipaddrport(1)';
		o.rmempty = false;

		o = s.option(form.DynamicList, 'allowed_group', _('Allowed multicast groups'));
		o.datatype = 'cidr4';
		o.rmempty = false;

		o = s.option(form.DynamicList, 'allowed_client', _('Allowed HTTP clients'));
		o.datatype = 'cidr4';
		o.rmempty = false;

		o = s.option(form.DynamicList, 'allowed_port', _('Allowed UDP ports'));
		o.datatype = 'or(port,portrange)';
		o.rmempty = false;

	for (const field of [
		['max_sessions', _('Maximum sessions'), 5],
		['max_clients', _('Maximum clients'), 5],
		['max_clients_per_session', _('Maximum clients per session'), 5],
		['max_clients_per_ip', _('Maximum clients per IP'), 5],
		['max_queue_bytes', _('Per-client queue bytes'), 524288],
		['playlist_max_bytes', _('Maximum playlist bytes'), 2097152]
	]) {
		o = s.option(form.Value, field[0], field[1]);
		o.datatype = 'uinteger';
		o.default = String(field[2]);
		o.rmempty = false;
	}

		o = s.option(form.Value, 'client_write_timeout', _('Client write timeout'));
		o.default = '2s';
		o.rmempty = false;

		o = s.option(form.Value, 'session_idle_grace', _('Session idle grace'));
		o.default = '1s';
		o.rmempty = false;

		o = s.option(form.Value, 'playlist_path', _('Playlist path'));
		o.placeholder = '/www/iptv.m3u8';

		o = s.option(form.Value, 'playlist_route', _('Playlist HTTP route'));
		o.default = '/playlist.m3u';
		o.rmempty = false;

		o = s.option(form.Button, '_upload_playlist', _('Upload playlist'));
		o.inputstyle = 'action';
		o.inputtitle = _('Upload…');
		o.onclick = function() {
			return ui.uploadFile('/tmp/mcastferry-playlist.upload')
				.then(function() { return callInstallPlaylist(); })
				.then(function(result) {
					if (!result.ok)
						throw new Error(_('Playlist installation failed with code %d').format(result.code));
					ui.addNotification(null, E('p', {}, _('Playlist installed at %s. Save the path and enable the service when ready.').format(result.path)), 'info');
				});
		};

		o = s.option(form.ListValue, 'log_level', _('Log level'));
		for (const level of ['debug', 'info', 'warn', 'error'])
			o.value(level, level);
		o.default = 'info';

		o = s.option(form.ListValue, 'log_format', _('Log format'));
		o.value('text', _('Text'));
		o.value('json', _('JSON'));
		o.default = 'text';

		return m.render();
	}
});
