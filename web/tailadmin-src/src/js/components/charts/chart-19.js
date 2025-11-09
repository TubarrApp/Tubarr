import ApexCharts from "apexcharts";

// ===== chartThree
const chart19 = () => {
  // const chartNineteenOptions = {
  //   series: [
  //     {
  //       name: "Sales",
  //       data: [180, 190, 170, 160, 175, 165, 170], // Reduced to 7 values for 7 days
  //     },
  //     {
  //       name: "Revenue",
  //       data: [40, 30, 50, 40, 55, 40, 70], // Reduced to 7 values for 7 days
  //     },
  //   ],
  //   legend: {
  //     show: false,
  //     position: "top",
  //     horizontalAlign: "left",
  //   },
  //   colors: ["#465FFF", "#9CB9FF"],
  //   chart: {
  //     fontFamily: "Outfit, sans-serif",
  //     height: 200,
  //     type: "area",
  //     toolbar: {
  //       show: false,
  //     },
  //     zoom: {
  //       enabled: false, // Disable all zoom functionality including mouse scroll zoom
  //     },
  //   },
  //   fill: {
  //     gradient: {
  //       enabled: true,
  //       opacityFrom: 0.55,
  //       opacityTo: 0,
  //     },
  //   },
  //   stroke: {
  //     curve: "straight",
  //     width: ["2", "2"],
  //   },
  //   markers: {
  //     size: 0,
  //   },
  //   labels: {
  //     show: false,
  //     position: "top",
  //   },
  //   grid: {
  //     xaxis: {
  //       lines: {
  //         show: false,
  //       },
  //     },
  //     yaxis: {
  //       lines: {
  //         show: true,
  //       },
  //     },
  //   },
  //   dataLabels: {
  //     enabled: false,
  //   },
  //   tooltip: {
  //     x: {
  //       format: "dd MMM yyyy",
  //     },
  //   },
  //   xaxis: {
  //     type: "category",
  //     categories: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"], // Changed to 7 days of the week
  //     axisBorder: {
  //       show: false,
  //     },
  //     axisTicks: {
  //       show: false,
  //     },
  //     tooltip: false,
  //   },
  //   yaxis: {
  //     title: {
  //       style: {
  //         fontSize: "0px",
  //       },
  //     },
  //     labels: {
  //       show: false, // This hides the y-axis values
  //     },
  //   },
  // };

  const chartNineteenOptions = {
    series: [
      {
        name: "Sales",
        data: [180, 190, 170, 160, 175, 165, 170], // Reduced to 7 values for 7 days
      },
      {
        name: "Revenue",
        data: [40, 30, 50, 40, 55, 40, 70], // Reduced to 7 values for 7 days
      },
    ],
    legend: {
      show: false,
      position: "top",
      horizontalAlign: "left",
    },
    colors: ["#465FFF", "#9CB9FF"],
    chart: {
      fontFamily: "Outfit, sans-serif",
      height: 200,
      type: "area",
      toolbar: {
        show: false,
      },
      zoom: {
        enabled: false, // Disable all zoom functionality including mouse scroll zoom
      },
    },
    fill: {
      gradient: {
        enabled: true,
        opacityFrom: 0.55,
        opacityTo: 0,
      },
    },
    stroke: {
      curve: "straight",
      width: ["2", "2"],
    },
    markers: {
      size: 0,
    },
    labels: {
      show: false,
      position: "top",
    },
    grid: {
      xaxis: {
        lines: {
          show: false,
        },
      },
      yaxis: {
        lines: {
          show: true,
        },
      },
    },
    dataLabels: {
      enabled: false,
    },
    tooltip: {
      x: {
        format: "dd MMM yyyy",
      },
    },
    xaxis: {
      type: "category",
      categories: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"], // Changed to 7 days of the week
      axisBorder: {
        show: false,
      },
      axisTicks: {
        show: false,
      },
      tooltip: false,
    },
    yaxis: {
      title: {
        style: {
          fontSize: "0px",
        },
      },
      labels: {
        show: true, // Changed to true to show y-axis values
        style: {
          colors: "#64748b", // Adding a subtle color
          fontSize: "12px",
          fontFamily: "Outfit, sans-serif",
        },
        formatter: function (value) {
          return Math.round(value); // Optional: rounds values to whole numbers
        },
      },
      min: 0, // Optional: ensures the y-axis starts at 0
      forceNiceScale: true, // Optional: ensures "nice" rounded numbers
    },
  };
  const chartSelector = document.querySelectorAll("#chartNineteen");

  if (chartSelector.length) {
    const chartNineteen = new ApexCharts(
      document.querySelector("#chartNineteen"),
      chartNineteenOptions,
    );
    chartNineteen.render();
  }
};

export default chart19;
