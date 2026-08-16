#!/usr/bin/env ucode

'use strict';

import { stat } from 'fs';
import { cursor } from 'uci';

const uci = cursor();

const methods = {
	status: {
		call: function() {
			const services = ubus.call('service', 'list', { name: 'mcastferry' }) || {};
			const playlist = stat('/www/iptv.m3u8');
			return {
				enabled: int(uci.get('mcastferry', 'main', 'enabled') || 0) == 1,
				service: services.mcastferry || {},
				playlist: playlist?.type == 'file' ? {
					path: '/www/iptv.m3u8',
					size: playlist.size,
					mtime: playlist.mtime
				} : null
			};
		}
	},
	install_playlist: {
		call: function() {
			const code = system('/usr/libexec/mcastferry-install-playlist');
			return {
				ok: code == 0,
				code: code,
				path: code == 0 ? '/www/iptv.m3u8' : null
			};
		}
	}
};

return { mcastferry: methods };
