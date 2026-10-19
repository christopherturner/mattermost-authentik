import React from 'react';

export default function AuthentikIcon({ style }) {
    return (
        <svg
            style={{ width: '20px', height: '20px', marginRight: '10px', verticalAlign: 'middle', flexShrink: 0, ...style }}
            viewBox="0 0 24 24"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
        >
            <path
                fill="currentColor"
                d="M12 1.5L2.5 7v10l9.5 5.5 9.5-5.5V7L12 1.5zm0 3.2l6.5 3.75v7.1L12 19.3l-6.5-3.75v-7.1L12 4.7zm0 2.5L7.8 15.2h2.2l.9-2.2h2.2l.9 2.2h2.2L12 7.2zm0 3.3l.8 1.9h-1.6l.8-1.9z"
            />
        </svg>
    );
}
